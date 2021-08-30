package stmgr

import (
	"context"
	"fmt"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/cron"
	"github.com/filecoin-project/lotus/chain/actors/builtin/reward"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/filecoin-project/specs-actors/v5/actors/builtin"
	cbg "github.com/whyrusleeping/cbor-gen"


	"sync"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"

	// Used for genesis.
	msig0 "github.com/filecoin-project/specs-actors/actors/builtin/multisig"
	"github.com/filecoin-project/specs-actors/v3/actors/migration/nv10"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin/paych"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/state"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
)

const LookbackNoLimit = api.LookbackNoLimit
const ReceiptAmtBitwidth = 3

var log = logging.Logger("statemgr")

type StateManagerAPI interface {
	Call(ctx context.Context, msg *types.Message, ts *types.TipSet) (*api.InvocResult, error)
	GetPaychState(ctx context.Context, addr address.Address, ts *types.TipSet) (*types.Actor, paych.State, error)
	LoadActorTsk(ctx context.Context, addr address.Address, tsk types.TipSetKey) (*types.Actor, error)
	LookupID(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error)
	ResolveToKeyAddress(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error)
}

type versionSpec struct {
	networkVersion network.Version
	atOrBelow      abi.ChainEpoch
}

type migration struct {
	upgrade       MigrationFunc
	preMigrations []PreMigration
	cache         *nv10.MemMigrationCache
}

type StateManager struct {
	cs *store.ChainStore

	cancel   context.CancelFunc
	shutdown chan struct{}

	// Determines the network version at any given epoch.
	networkVersions []versionSpec
	latestVersion   network.Version

	// Maps chain epochs to migrations.
	stateMigrations map[abi.ChainEpoch]*migration
	// A set of potentially expensive/time consuming upgrades. Explicit
	// calls for, e.g., gas estimation fail against this epoch with
	// ErrExpensiveFork.
	expensiveUpgrades map[abi.ChainEpoch]struct{}

	stCache             map[string][]cid.Cid
	tCache              treeCache
	compWait            map[string]chan struct{}
	stlk                sync.Mutex
	genesisMsigLk       sync.Mutex
	newVM               func(context.Context, *vm.VMOpts) (*vm.VM, error)
	syscalls            vm.SyscallBuilder
	preIgnitionVesting  []msig0.State
	postIgnitionVesting []msig0.State
	postCalicoVesting   []msig0.State

	genesisPledge      abi.TokenAmount
	genesisMarketFunds abi.TokenAmount

	tsExecMonitor ExecMonitor
}

// Caches a single state tree
type treeCache struct {
	root cid.Cid
	tree *state.StateTree
}

func NewStateManager(cs *store.ChainStore, sys vm.SyscallBuilder) *StateManager {
	sm, err := NewStateManagerWithUpgradeSchedule(cs, sys, DefaultUpgradeSchedule())
	if err != nil {
		panic(fmt.Sprintf("default upgrade schedule is invalid: %s", err))
	}
	return sm
}

func NewStateManagerWithUpgradeSchedule(cs *store.ChainStore, sys vm.SyscallBuilder, us UpgradeSchedule) (*StateManager, error) {
	// If we have upgrades, make sure they're in-order and make sense.
	if err := us.Validate(); err != nil {
		return nil, err
	}

	stateMigrations := make(map[abi.ChainEpoch]*migration, len(us))
	expensiveUpgrades := make(map[abi.ChainEpoch]struct{}, len(us))
	var networkVersions []versionSpec
	lastVersion := network.Version0
	if len(us) > 0 {
		// If we have any upgrades, process them and create a version
		// schedule.
		for _, upgrade := range us {
			if upgrade.Migration != nil || upgrade.PreMigrations != nil {
				migration := &migration{
					upgrade:       upgrade.Migration,
					preMigrations: upgrade.PreMigrations,
					cache:         nv10.NewMemMigrationCache(),
				}
				stateMigrations[upgrade.Height] = migration
			}
			if upgrade.Expensive {
				expensiveUpgrades[upgrade.Height] = struct{}{}
			}
			networkVersions = append(networkVersions, versionSpec{
				networkVersion: lastVersion,
				atOrBelow:      upgrade.Height,
			})
			lastVersion = upgrade.Network
		}
	} else {
		// Otherwise, go directly to the latest version.
		lastVersion = build.NewestNetworkVersion
	}

	return &StateManager{
		networkVersions:   networkVersions,
		latestVersion:     lastVersion,
		stateMigrations:   stateMigrations,
		expensiveUpgrades: expensiveUpgrades,
		newVM:             vm.NewVM,
		syscalls:          sys,
		cs:                cs,
		stCache:           make(map[string][]cid.Cid),
		tCache: treeCache{
			root: cid.Undef,
			tree: nil,
		},
		compWait: make(map[string]chan struct{}),
	}, nil
}

func NewStateManagerWithUpgradeScheduleAndMonitor(cs *store.ChainStore, sys vm.SyscallBuilder, us UpgradeSchedule, em ExecMonitor) (*StateManager, error) {
	sm, err := NewStateManagerWithUpgradeSchedule(cs, sys, us)
	if err != nil {
		return nil, err
	}
	sm.tsExecMonitor = em
	return sm, nil
}

func cidsToKey(cids []cid.Cid) string {
	var out string
	for _, c := range cids {
		out += c.KeyString()
	}
	return out
}

// Start starts the state manager's optional background processes. At the moment, this schedules
// pre-migration functions to run ahead of network upgrades.
//
// This method is not safe to invoke from multiple threads or concurrently with Stop.
func (sm *StateManager) Start(context.Context) error {
	var ctx context.Context
	ctx, sm.cancel = context.WithCancel(context.Background())
	sm.shutdown = make(chan struct{})
	go sm.preMigrationWorker(ctx)
	return nil
}

// Stop starts the state manager's background processes.
//
// This method is not safe to invoke concurrently with Start.
func (sm *StateManager) Stop(ctx context.Context) error {
	if sm.cancel != nil {
		sm.cancel()
		select {
		case <-sm.shutdown:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

//func (sm *StateManager) TipSetState(ctx context.Context, ts *types.TipSet) (st cid.Cid, rec cid.Cid, err error) {
//	ctx, span := trace.StartSpan(ctx, "tipSetState")
//	defer span.End()
//	if span.IsRecordingEvents() {
//		span.AddAttributes(trace.StringAttribute("tipset", fmt.Sprint(ts.Cids())))
//	}
//
//	ck := cidsToKey(ts.Cids())
//	sm.stlk.Lock()
//	cw, cwok := sm.compWait[ck]
//	if cwok {
//		sm.stlk.Unlock()
//		span.AddAttributes(trace.BoolAttribute("waited", true))
//		select {
//		case <-cw:
//			sm.stlk.Lock()
//		case <-ctx.Done():
//			return cid.Undef, cid.Undef, ctx.Err()
//		}
//	}
//	cached, ok := sm.stCache[ck]
//	if ok {
//		sm.stlk.Unlock()
//		span.AddAttributes(trace.BoolAttribute("cache", true))
//		return cached[0], cached[1], nil
//	}
//	ch := make(chan struct{})
//	sm.compWait[ck] = ch
//
//	defer func() {
//		sm.stlk.Lock()
//		delete(sm.compWait, ck)
//		if st != cid.Undef {
//			sm.stCache[ck] = []cid.Cid{st, rec}
//		}
//		sm.stlk.Unlock()
//		close(ch)
//	}()
//
//	sm.stlk.Unlock()
//
//	if ts.Height() == 0 {
//		// NB: This is here because the process that executes blocks requires that the
//		// block miner reference a valid miner in the state tree. Unless we create some
//		// magical genesis miner, this won't work properly, so we short circuit here
//		// This avoids the question of 'who gets paid the genesis block reward'
//		return ts.Blocks()[0].ParentStateRoot, ts.Blocks()[0].ParentMessageReceipts, nil
//	}
//
//	st, rec, err = sm.computeTipSetState(ctx, ts, nil)
//	if err != nil {
//		return cid.Undef, cid.Undef, err
//	}
//
//	return st, rec, nil
//}

func traceFunc(trace *[]*api.InvocResult) func(mcid cid.Cid, msg *types.Message, ret *vm.ApplyRet) error {
	return func(mcid cid.Cid, msg *types.Message, ret *vm.ApplyRet) error {
		ir := &api.InvocResult{
			MsgCid:         mcid,
			Msg:            msg,
			MsgRct:         &ret.MessageReceipt,
			ExecutionTrace: ret.ExecutionTrace,
			Duration:       ret.Duration,
		}
		if ret.ActorErr != nil {
			ir.Error = ret.ActorErr.Error()
		}
		if ret.GasCosts != nil {
			ir.GasCost = MakeMsgGasCost(msg, ret)
		}
		*trace = append(*trace, ir)
		return nil
	}
}

//func (sm *StateManager) ExecutionTraceWithMonitor(ctx context.Context, ts *types.TipSet, em ExecMonitor) (cid.Cid, error) {
//	st, _, err := sm.computeTipSetState(ctx, ts, em)
//	return st, err
//}

//func (sm *StateManager) ExecutionTrace(ctx context.Context, ts *types.TipSet) (cid.Cid, []*api.InvocResult, error) {
//	var traces []*api.InvocResult
//	//st, err := sm.ExecutionTraceWithMonitor(ctx, ts, &InvocationTracer{trace: &invocTrace})
//	st, _, err := sm.computeTipSetState(ctx, ts,  &InvocationTracer{trace: &traces}  )
//
//	//st, _, err := sm.computeTipSetState(ctx, ts, traceFunc(&trace))
//	if err != nil {
//		return cid.Undef, nil, err
//	}
//
//	return st, traces, nil
//}

type ExecCallback func(cid.Cid, *types.Message, *vm.ApplyRet) error

//
//func (sm *StateManager) ApplyBlocks(ctx context.Context, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []store.BlockMessages, epoch abi.ChainEpoch, r vm.Rand, em ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet) (cid.Cid, cid.Cid, error) {
//	done := metrics.Timer(ctx, metrics.VMApplyBlocksTotal)
//	defer done()
//
//	partDone := metrics.Timer(ctx, metrics.VMApplyEarly)
//	defer func() {
//		partDone()
//	}()
//
//	makeVmWithBaseState := func(base cid.Cid) (*vm.VM, error) {
//		vmopt := &vm.VMOpts{
//			StateBase:      base,
//			Epoch:          epoch,
//			Rand:           r,
//			Bstore:         sm.cs.StateBlockstore(),
//			Syscalls:       sm.cs.VMSys(),
//			CircSupplyCalc: sm.GetVMCirculatingSupply,
//			NtwkVersion:    sm.GetNtwkVersion,
//			BaseFee:        baseFee,
//			LookbackState:  LookbackStateGetterForTipset(sm, ts),
//		}
//
//		return sm.newVM(ctx, vmopt)
//	}
//
//	vmi, err := makeVmWithBaseState(pstate)
//	if err != nil {
//		return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
//	}
//
//	runCron := func(epoch abi.ChainEpoch) error {
//		cronMsg := &types.Message{
//			To:         cron.Address,
//			From:       builtin.SystemActorAddr,
//			Nonce:      uint64(epoch),
//			Value:      types.NewInt(0),
//			GasFeeCap:  types.NewInt(0),
//			GasPremium: types.NewInt(0),
//			GasLimit:   build.BlockGasLimit * 10000, // Make super sure this is never too little
//			Method:     cron.Methods.EpochTick,
//			Params:     nil,
//		}
//		ret, err := vmi.ApplyImplicitMessage(ctx, cronMsg)
//		if err != nil {
//			return err
//		}
//		if em != nil {
//			if err := em.MessageApplied(ctx, ts, cronMsg.Cid(), cronMsg, ret, true); err != nil {
//				return xerrors.Errorf("callback failed on cron message: %w", err)
//			}
//		}
//		if ret.ExitCode != 0 {
//			return xerrors.Errorf("CheckProofSubmissions exit was non-zero: %d", ret.ExitCode)
//		}
//
//		return nil
//	}
//
//	for i := parentEpoch; i < epoch; i++ {
//		if i > parentEpoch {
//			// run cron for null rounds if any
//			if err := runCron(i); err != nil {
//				return cid.Undef, cid.Undef, err
//			}
//
//			pstate, err = vmi.Flush(ctx)
//			if err != nil {
//				return cid.Undef, cid.Undef, xerrors.Errorf("flushing vm: %w", err)
//			}
//		}
//
//		// handle state forks
//		// XXX: The state tree
//		newState, err := sm.handleStateForks(ctx, pstate, i, em, ts)
//		if err != nil {
//			return cid.Undef, cid.Undef, xerrors.Errorf("error handling state forks: %w", err)
//		}
//
//		if pstate != newState {
//			vmi, err = makeVmWithBaseState(newState)
//			if err != nil {
//				return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
//			}
//		}
//
//		vmi.SetBlockHeight(i + 1)
//		pstate = newState
//	}
//
//	partDone()
//	partDone = metrics.Timer(ctx, metrics.VMApplyMessages)
//
//	var receipts []cbg.CBORMarshaler
//	processedMsgs := make(map[cid.Cid]struct{})
//	for _, b := range bms {
//		penalty := types.NewInt(0)
//		gasReward := big.Zero()
//
//		for _, cm := range append(b.BlsMessages, b.SecpkMessages...) {
//			m := cm.VMMessage()
//			if _, found := processedMsgs[m.Cid()]; found {
//				continue
//			}
//			r, err := vmi.ApplyMessage(ctx, cm)
//			if err != nil {
//				return cid.Undef, cid.Undef, err
//			}
//
//			receipts = append(receipts, &r.MessageReceipt)
//			gasReward = big.Add(gasReward, r.GasCosts.MinerTip)
//			penalty = big.Add(penalty, r.GasCosts.MinerPenalty)
//
//			if em != nil {
//				if err := em.MessageApplied(ctx, ts, cm.Cid(), m, r, false); err != nil {
//					return cid.Undef, cid.Undef, err
//				}
//			}
//			processedMsgs[m.Cid()] = struct{}{}
//		}
//
//		params, err := actors.SerializeParams(&reward.AwardBlockRewardParams{
//			Miner:     b.Miner,
//			Penalty:   penalty,
//			GasReward: gasReward,
//			WinCount:  b.WinCount,
//		})
//		if err != nil {
//			return cid.Undef, cid.Undef, xerrors.Errorf("failed to serialize award params: %w", err)
//		}
//
//		rwMsg := &types.Message{
//			From:       builtin.SystemActorAddr,
//			To:         reward.Address,
//			Nonce:      uint64(epoch),
//			Value:      types.NewInt(0),
//			GasFeeCap:  types.NewInt(0),
//			GasPremium: types.NewInt(0),
//			GasLimit:   1 << 30,
//			Method:     reward.Methods.AwardBlockReward,
//			Params:     params,
//		}
//		ret, actErr := vmi.ApplyImplicitMessage(ctx, rwMsg)
//		if actErr != nil {
//			return cid.Undef, cid.Undef, xerrors.Errorf("failed to apply reward message for miner %s: %w", b.Miner, actErr)
//		}
//		if em != nil {
//			if err := em.MessageApplied(ctx, ts, rwMsg.Cid(), rwMsg, ret, true); err != nil {
//				return cid.Undef, cid.Undef, xerrors.Errorf("callback failed on reward message: %w", err)
//			}
//		}
//
//		if ret.ExitCode != 0 {
//			return cid.Undef, cid.Undef, xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
//		}
//	}
//
//	partDone()
//	partDone = metrics.Timer(ctx, metrics.VMApplyCron)
//
//	if err := runCron(epoch); err != nil {
//		return cid.Cid{}, cid.Cid{}, err
//	}
//
//	partDone()
//	partDone = metrics.Timer(ctx, metrics.VMApplyFlush)
//
//	rectarr := blockadt.MakeEmptyArray(sm.cs.ActorStore(ctx))
//	for i, receipt := range receipts {
//		if err := rectarr.Set(uint64(i), receipt); err != nil {
//			return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
//		}
//	}
//	rectroot, err := rectarr.Root()
//	if err != nil {
//		return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
//	}
//
//	st, err := vmi.Flush(ctx)
//	if err != nil {
//		return cid.Undef, cid.Undef, xerrors.Errorf("vm flush failed: %w", err)
//	}
//
//	stats.Record(ctx, metrics.VMSends.M(int64(atomic.LoadUint64(&vm.StatSends))),
//		metrics.VMApplied.M(int64(atomic.LoadUint64(&vm.StatApplied))))
//
//	return st, rectroot, nil
//}


func (sm *StateManager) ApplyBlocksExt(ctx context.Context, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []store.BlockMessages, epoch abi.ChainEpoch, r vm.Rand, em ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet) (cid.Cid, cid.Cid, error) {



	makeVmWithBaseState := func(base cid.Cid) (*vm.VM, error) {
		vmopt := &vm.VMOpts{
			StateBase:      base,
			Epoch:          epoch,
			Rand:           r,
			Bstore:         sm.cs.StateBlockstore(),
			Syscalls:       sm.syscalls,
			CircSupplyCalc: sm.GetVMCirculatingSupply,
			NtwkVersion:    sm.GetNtwkVersion,
			BaseFee:        baseFee,
			LookbackState:  LookbackStateGetterForTipset(sm, ts),
		}

		return sm.newVM(ctx, vmopt)
	}

	vmi, err := makeVmWithBaseState(pstate)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
	}

	runCron := func(epoch abi.ChainEpoch) error {

		cronMsg := &types.Message{
			To:         cron.Address,
			From:       builtin.SystemActorAddr,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   build.BlockGasLimit * 10000, // Make super sure this is never too little
			Method:     cron.Methods.EpochTick,
			Params:     nil,
		}
		ret, err := vmi.ApplyImplicitMessage(ctx, cronMsg)
		if err != nil {
			return err
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, cronMsg.Cid(), cronMsg, ret, true); err != nil {
				return xerrors.Errorf("callback failed on cron message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return xerrors.Errorf("CheckProofSubmissions exit was non-zero: %d", ret.ExitCode)
		}

		return nil
	}

	for i := parentEpoch; i < epoch; i++ {
		if i > parentEpoch {
			// run cron for null rounds if any
			if err := runCron(i); err != nil {
				return cid.Undef, cid.Undef, err
			}

			pstate, err = vmi.Flush(ctx)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("flushing vm: %w", err)
			}
		}

		// handle state forks
		// XXX: The state tree
		newState, err := sm.handleStateForks(ctx, pstate, i, em, ts)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("error handling state forks: %w", err)
		}

		if pstate != newState {
			vmi, err = makeVmWithBaseState(newState)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
			}
		}

		vmi.SetBlockHeight(i + 1)
		pstate = newState
	}

	var receipts []cbg.CBORMarshaler
	processedMsgs := make(map[cid.Cid]struct{})
	for _, b := range bms {
		penalty := types.NewInt(0)
		gasReward := big.Zero()

		for _, cm := range append(b.BlsMessages, b.SecpkMessages...) {
			m := cm.VMMessage()
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			r, err := vmi.ApplyMessage(ctx, cm)
			if err != nil {
				return cid.Undef, cid.Undef, err
			}

			receipts = append(receipts, &r.MessageReceipt)
			gasReward = big.Add(gasReward, r.GasCosts.MinerTip)
			penalty = big.Add(penalty, r.GasCosts.MinerPenalty)

			if em != nil {
				if err := em.MessageApplied(ctx, ts, cm.Cid(), m, r, false); err != nil {
					return cid.Undef, cid.Undef, err
				}
			}
			processedMsgs[m.Cid()] = struct{}{}
		}

		params, err := actors.SerializeParams(&reward.AwardBlockRewardParams{
			Miner:     b.Miner,
			Penalty:   penalty,
			GasReward: gasReward,
			WinCount:  b.WinCount,
		})
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to serialize award params: %w", err)
		}

		rwMsg := &types.Message{
			From:       builtin.SystemActorAddr,
			To:         reward.Address,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   1 << 30,
			Method:     reward.Methods.AwardBlockReward,
			Params:     params,
		}
		ret, actErr := vmi.ApplyImplicitMessage(ctx, rwMsg)
		if actErr != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to apply reward message for miner %s: %w", b.Miner, actErr)
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, rwMsg.Cid(), rwMsg, ret, true); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("callback failed on reward message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return cid.Undef, cid.Undef, xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
		}
	}


	if err := runCron(epoch); err != nil {
		return cid.Cid{}, cid.Cid{}, err
	}


	// XXX: Is the height correct? Or should it be epoch-1?
	rectarr := blockadt.MakeEmptyArray(sm.cs.ActorStore(ctx))
	for i, receipt := range receipts {
		if err := rectarr.Set(uint64(i), receipt); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
		}
	}
	rectroot, err := rectarr.Root()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
	}
	st, err := vmi.Flush(ctx)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("vm flush failed: %w", err)
	}



	return st, rectroot, nil
}


func (sm *StateManager) ApplyMessages(ctx context.Context, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []store.BlockMessages, epoch abi.ChainEpoch, r vm.Rand, em ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet,mid cid.Cid) (cid.Cid, cid.Cid, error) {

	makeVmWithBaseState := func(base cid.Cid) (*vm.VM, error) {
		vmopt := &vm.VMOpts{
			StateBase:      base,
			Epoch:          epoch,
			Rand:           r,
			Bstore:         sm.cs.StateBlockstore(),
			Syscalls:       sm.syscalls,
			CircSupplyCalc: sm.GetVMCirculatingSupply,
			NtwkVersion:    sm.GetNtwkVersion,
			BaseFee:        baseFee,
			LookbackState:  LookbackStateGetterForTipset(sm, ts),
		}

		return sm.newVM(ctx, vmopt)
	}

	vmi, err := makeVmWithBaseState(pstate)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
	}

	runCron := func(epoch abi.ChainEpoch) error {

		cronMsg := &types.Message{
			To:         cron.Address,
			From:       builtin.SystemActorAddr,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   build.BlockGasLimit * 10000, // Make super sure this is never too little
			Method:     cron.Methods.EpochTick,
			Params:     nil,
		}
		ret, err := vmi.ApplyImplicitMessage(ctx, cronMsg)
		if err != nil {
			return err
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, cronMsg.Cid(), cronMsg, ret, true); err != nil {
				return xerrors.Errorf("callback failed on cron message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return xerrors.Errorf("CheckProofSubmissions exit was non-zero: %d", ret.ExitCode)
		}

		return nil
	}

	for i := parentEpoch; i < epoch; i++ {
		if i > parentEpoch {
			// run cron for null rounds if any
			if err := runCron(i); err != nil {
				return cid.Undef, cid.Undef, err
			}

			pstate, err = vmi.Flush(ctx)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("flushing vm: %w", err)
			}
		}

		// handle state forks
		// XXX: The state tree
		newState, err := sm.handleStateForks(ctx, pstate, i, em, ts)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("error handling state forks: %w", err)
		}

		if pstate != newState {
			vmi, err = makeVmWithBaseState(newState)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
			}
		}

		vmi.SetBlockHeight(i + 1)
		pstate = newState
	}

	var receipts []cbg.CBORMarshaler
	processedMsgs := make(map[cid.Cid]struct{})
	for _, b := range bms {
		penalty := types.NewInt(0)
		gasReward := big.Zero()

		for _, cm := range append(b.BlsMessages, b.SecpkMessages...) {
			//fmt.Println("MAgCid",cm.Cid())
			//fmt.Println("MAgVMMessageCid",cm.VMMessage().Cid())
			//compare := strings.Compare(mid.String(), cm.Cid().String())
			//compare1 := strings.Compare(mid.String(), cm.VMMessage().Cid().String())
			//if compare == 0 || compare1 == 0 {
			//	fmt.Println("MAg1Cid",cm.Cid())
			//	fmt.Println("MAg1VMMessageCid",cm.VMMessage().Cid())
			//	fmt.Println("mid1",mid)

			m := cm.VMMessage()
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			r, err := vmi.ApplyMessage(ctx, cm)
			if err != nil {
				return cid.Undef, cid.Undef, err
			}

			receipts = append(receipts, &r.MessageReceipt)
			gasReward = big.Add(gasReward, r.GasCosts.MinerTip)
			penalty = big.Add(penalty, r.GasCosts.MinerPenalty)

			if em != nil {
				if err := em.MessageApplied(ctx, ts, cm.Cid(), m, r, false); err != nil {
					return cid.Undef, cid.Undef, err
				}
			}
			processedMsgs[m.Cid()] = struct{}{}
			//}else {
			//	fmt.Println("MAg2Cid",cm.Cid())
			//	fmt.Println("MAg2VMMessageCid",cm.VMMessage().Cid())
			//	fmt.Println("mid2",mid)
			//
			//}


		}

		params, err := actors.SerializeParams(&reward.AwardBlockRewardParams{
			Miner:     b.Miner,
			Penalty:   penalty,
			GasReward: gasReward,
			WinCount:  b.WinCount,
		})
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to serialize award params: %w", err)
		}

		rwMsg := &types.Message{
			From:       builtin.SystemActorAddr,
			To:         reward.Address,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   1 << 30,
			Method:     reward.Methods.AwardBlockReward,
			Params:     params,
		}
		ret, actErr := vmi.ApplyImplicitMessage(ctx, rwMsg)
		if actErr != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to apply reward message for miner %s: %w", b.Miner, actErr)
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, rwMsg.Cid(), rwMsg, ret, true); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("callback failed on reward message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return cid.Undef, cid.Undef, xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
		}
	}

	if err := runCron(epoch); err != nil {
		return cid.Cid{}, cid.Cid{}, err
	}

	// XXX: Is the height correct? Or should it be epoch-1?
	rectarr := blockadt.MakeEmptyArray(sm.cs.ActorStore(ctx))
	for i, receipt := range receipts {
		if err := rectarr.Set(uint64(i), receipt); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
		}
	}
	rectroot, err := rectarr.Root()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
	}
	st, err := vmi.Flush(ctx)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("vm flush failed: %w", err)
	}

	return st, rectroot, nil
}


func (sm *StateManager) ApplyMessagesEx(ctx context.Context, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []store.BlockMessages, epoch abi.ChainEpoch, r vm.Rand, em ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet,mid cid.Cid) (cid.Cid, cid.Cid, error) {

	makeVmWithBaseState := func(base cid.Cid) (*vm.VM, error) {
		vmopt := &vm.VMOpts{
			StateBase:      base,
			Epoch:          epoch,
			Rand:           r,
			Bstore:         sm.cs.StateBlockstore(),
			Syscalls:       sm.syscalls,
			CircSupplyCalc: sm.GetVMCirculatingSupply,
			NtwkVersion:    sm.GetNtwkVersion,
			BaseFee:        baseFee,
			LookbackState:  LookbackStateGetterForTipset(sm, ts),
		}

		return sm.newVM(ctx, vmopt)
	}

	vmi, err := makeVmWithBaseState(pstate)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
	}

	runCron := func(epoch abi.ChainEpoch) error {

		cronMsg := &types.Message{
			To:         cron.Address,
			From:       builtin.SystemActorAddr,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   build.BlockGasLimit * 10000, // Make super sure this is never too little
			Method:     cron.Methods.EpochTick,
			Params:     nil,
		}
		ret, err := vmi.ApplyImplicitMessage(ctx, cronMsg)
		if err != nil {
			return err
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, cronMsg.Cid(), cronMsg, ret, true); err != nil {
				return xerrors.Errorf("callback failed on cron message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return xerrors.Errorf("CheckProofSubmissions exit was non-zero: %d", ret.ExitCode)
		}

		return nil
	}

	for i := parentEpoch; i < epoch; i++ {
		if i > parentEpoch {
			// run cron for null rounds if any
			if err := runCron(i); err != nil {
				return cid.Undef, cid.Undef, err
			}

			pstate, err = vmi.Flush(ctx)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("flushing vm: %w", err)
			}
		}

		// handle state forks
		// XXX: The state tree
		newState, err := sm.handleStateForks(ctx, pstate, i, em, ts)
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("error handling state forks: %w", err)
		}

		if pstate != newState {
			vmi, err = makeVmWithBaseState(newState)
			if err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("making vm: %w", err)
			}
		}

		vmi.SetBlockHeight(i + 1)
		pstate = newState
	}

	var receipts []cbg.CBORMarshaler
	processedMsgs := make(map[cid.Cid]struct{})
	for _, b := range bms {
		penalty := types.NewInt(0)
		gasReward := big.Zero()

		for _, cm := range append(b.BlsMessages, b.SecpkMessages...) {
			//fmt.Println("MAgCid",cm.Cid())
			//fmt.Println("MAgVMMessageCid",cm.VMMessage().Cid())
			//compare := strings.Compare(mid.String(), cm.Cid().String())
			//compare1 := strings.Compare(mid.String(), cm.VMMessage().Cid().String())
			//if compare == 0 || compare1 == 0 {
			//	fmt.Println("MAg1Cid",cm.Cid())
			//	fmt.Println("MAg1VMMessageCid",cm.VMMessage().Cid())
			//	fmt.Println("mid1",mid)

			m := cm.VMMessage()
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			r, err := vmi.ApplyMessage(ctx, cm)
			if err != nil {
				return cid.Undef, cid.Undef, err
			}

			receipts = append(receipts, &r.MessageReceipt)
			gasReward = big.Add(gasReward, r.GasCosts.MinerTip)
			penalty = big.Add(penalty, r.GasCosts.MinerPenalty)

			if em != nil {
				if err := em.MessageApplied(ctx, ts, cm.Cid(), m, r, false); err != nil {
					return cid.Undef, cid.Undef, err
				}
			}
			processedMsgs[m.Cid()] = struct{}{}
			//}else {
			//	fmt.Println("MAg2Cid",cm.Cid())
			//	fmt.Println("MAg2VMMessageCid",cm.VMMessage().Cid())
			//	fmt.Println("mid2",mid)
			//
			//}


		}

		params, err := actors.SerializeParams(&reward.AwardBlockRewardParams{
			Miner:     b.Miner,
			Penalty:   penalty,
			GasReward: gasReward,
			WinCount:  b.WinCount,
		})
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to serialize award params: %w", err)
		}

		rwMsg := &types.Message{
			From:       builtin.SystemActorAddr,
			To:         reward.Address,
			Nonce:      uint64(epoch),
			Value:      types.NewInt(0),
			GasFeeCap:  types.NewInt(0),
			GasPremium: types.NewInt(0),
			GasLimit:   1 << 30,
			Method:     reward.Methods.AwardBlockReward,
			Params:     params,
		}
		ret, actErr := vmi.ApplyImplicitMessage(ctx, rwMsg)
		if actErr != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to apply reward message for miner %s: %w", b.Miner, actErr)
		}
		if em != nil {
			if err := em.MessageApplied(ctx, ts, rwMsg.Cid(), rwMsg, ret, true); err != nil {
				return cid.Undef, cid.Undef, xerrors.Errorf("callback failed on reward message: %w", err)
			}
		}
		if ret.ExitCode != 0 {
			return cid.Undef, cid.Undef, xerrors.Errorf("reward application message failed (exit %d): %s", ret.ExitCode, ret.ActorErr)
		}
	}

	if err := runCron(epoch); err != nil {
		return cid.Cid{}, cid.Cid{}, err
	}

	rectarr := blockadt.MakeEmptyArray(sm.cs.ActorStore(ctx))
	for i, receipt := range receipts {
		if err := rectarr.Set(uint64(i), receipt); err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
		}
	}
	rectroot, err := rectarr.Root()
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
	}

	st, err := vmi.Flush(ctx)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("vm flush failed: %w", err)
	}

	return st, rectroot, nil
}
//
//func (sm *StateManager) computeTipSetState(ctx context.Context, ts *types.TipSet, em ExecMonitor) (cid.Cid, cid.Cid, error) {
//	ctx, span := trace.StartSpan(ctx, "computeTipSetState")
//	defer span.End()
//
//	blks := ts.Blocks()
//
//	for i := 0; i < len(blks); i++ {
//		for j := i + 1; j < len(blks); j++ {
//			if blks[i].Miner == blks[j].Miner {
//				return cid.Undef, cid.Undef,
//					xerrors.Errorf("duplicate miner in a tipset (%s %s)",
//						blks[i].Miner, blks[j].Miner)
//			}
//		}
//	}
//
//	var parentEpoch abi.ChainEpoch
//	pstate := blks[0].ParentStateRoot
//	if blks[0].Height > 0 {
//		parent, err := sm.cs.GetBlock(blks[0].Parents[0])
//		if err != nil {
//			return cid.Undef, cid.Undef, xerrors.Errorf("getting parent block: %w", err)
//		}
//
//		parentEpoch = parent.Height
//	}
//
//	r := store.NewChainRand(sm.cs, ts.Cids())
//
//	blkmsgs, err := sm.cs.BlockMsgsForTipset(ts)
//	if err != nil {
//		return cid.Undef, cid.Undef, xerrors.Errorf("getting block messages for tipset: %w", err)
//	}
//
//	baseFee := blks[0].ParentBaseFee
//
//	return sm.ApplyBlocks(ctx, parentEpoch, pstate, blkmsgs, blks[0].Height, r, em, baseFee, ts)
//}




//func (sm *StateManager) parentState(ts *types.TipSet) cid.Cid {
//	if ts == nil {
//		ts = sm.cs.GetHeaviestTipSet()
//	}
//
//	return ts.ParentState()
//}

func (sm *StateManager) ChainStore() *store.ChainStore {
	return sm.cs
}

// ResolveToKeyAddress is similar to `vm.ResolveToKeyAddr` but does not allow `Actor` type of addresses.
// Uses the `TipSet` `ts` to generate the VM state.
func (sm *StateManager) ResolveToKeyAddress(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error) {
	switch addr.Protocol() {
	case address.BLS, address.SECP256K1:
		return addr, nil
	case address.Actor:
		return address.Undef, xerrors.New("cannot resolve actor address to key address")
	default:
	}

	if ts == nil {
		ts = sm.cs.GetHeaviestTipSet()
	}

	cst := cbor.NewCborStore(sm.cs.StateBlockstore())

	// First try to resolve the actor in the parent state, so we don't have to compute anything.
	tree, err := state.LoadStateTree(cst, ts.ParentState())
	if err != nil {
		return address.Undef, xerrors.Errorf("failed to load parent state tree at tipset %s: %w", ts.Parents(), err)
	}

	resolved, err := vm.ResolveToKeyAddr(tree, cst, addr)
	if err == nil {
		return resolved, nil
	}

	// If that fails, compute the tip-set and try again.
	st, _, err := sm.TipSetState(ctx, ts)
	if err != nil {
		return address.Undef, xerrors.Errorf("resolve address failed to get tipset %s state: %w", ts, err)
	}

	tree, err = state.LoadStateTree(cst, st)
	if err != nil {
		return address.Undef, xerrors.Errorf("failed to load state tree at tipset %s: %w", ts, err)
	}

	return vm.ResolveToKeyAddr(tree, cst, addr)
}

// ResolveToKeyAddressAtFinality is similar to stmgr.ResolveToKeyAddress but fails if the ID address being resolved isn't reorg-stable yet.
// It should not be used for consensus-critical subsystems.
func (sm *StateManager) ResolveToKeyAddressAtFinality(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error) {
	switch addr.Protocol() {
	case address.BLS, address.SECP256K1:
		return addr, nil
	case address.Actor:
		return address.Undef, xerrors.New("cannot resolve actor address to key address")
	default:
	}

	if ts == nil {
		ts = sm.cs.GetHeaviestTipSet()
	}

	var err error
	if ts.Height() > policy.ChainFinality {
		ts, err = sm.ChainStore().GetTipsetByHeight(ctx, ts.Height()-policy.ChainFinality, ts, true)
		if err != nil {
			return address.Undef, xerrors.Errorf("failed to load lookback tipset: %w", err)
		}
	}

	cst := cbor.NewCborStore(sm.cs.StateBlockstore())
	tree := sm.tCache.tree

	if tree == nil || sm.tCache.root != ts.ParentState() {
		tree, err = state.LoadStateTree(cst, ts.ParentState())
		if err != nil {
			return address.Undef, xerrors.Errorf("failed to load parent state tree: %w", err)
		}

		sm.tCache = treeCache{
			root: ts.ParentState(),
			tree: tree,
		}
	}

	resolved, err := vm.ResolveToKeyAddr(tree, cst, addr)
	if err == nil {
		return resolved, nil
	}

	return address.Undef, xerrors.New("ID address not found in lookback state")
}

func (sm *StateManager) GetBlsPublicKey(ctx context.Context, addr address.Address, ts *types.TipSet) (pubk []byte, err error) {
	kaddr, err := sm.ResolveToKeyAddress(ctx, addr, ts)
	if err != nil {
		return pubk, xerrors.Errorf("failed to resolve address to key address: %w", err)
	}

	if kaddr.Protocol() != address.BLS {
		return pubk, xerrors.Errorf("address must be BLS address to load bls public key")
	}

	return kaddr.Payload(), nil
}

func (sm *StateManager) LookupID(ctx context.Context, addr address.Address, ts *types.TipSet) (address.Address, error) {
	cst := cbor.NewCborStore(sm.cs.StateBlockstore())
	state, err := state.LoadStateTree(cst, sm.parentState(ts))
	if err != nil {
		return address.Undef, xerrors.Errorf("load state tree: %w", err)
	}
	return state.LookupID(addr)
}

func (sm *StateManager) ValidateChain(ctx context.Context, ts *types.TipSet) error {
	tschain := []*types.TipSet{ts}
	for ts.Height() != 0 {
		next, err := sm.cs.LoadTipSet(ts.Parents())
		if err != nil {
			return err
		}

		tschain = append(tschain, next)
		ts = next
	}

	lastState := tschain[len(tschain)-1].ParentState()
	for i := len(tschain) - 1; i >= 0; i-- {
		cur := tschain[i]
		log.Infof("computing state (height: %d, ts=%s)", cur.Height(), cur.Cids())
		if cur.ParentState() != lastState {
			return xerrors.Errorf("tipset chain had state mismatch at height %d", cur.Height())
		}
		st, _, err := sm.TipSetState(ctx, cur)
		if err != nil {
			return err
		}
		lastState = st
	}

	return nil
}

func (sm *StateManager) SetVMConstructor(nvm func(context.Context, *vm.VMOpts) (*vm.VM, error)) {
	sm.newVM = nvm
}

func (sm *StateManager) GetNtwkVersion(ctx context.Context, height abi.ChainEpoch) network.Version {
	// The epochs here are the _last_ epoch for every version, or -1 if the
	// version is disabled.
	for _, spec := range sm.networkVersions {
		if height <= spec.atOrBelow {
			return spec.networkVersion
		}
	}
	return sm.latestVersion
}

func (sm *StateManager) GetRewardState(ctx context.Context, ts *types.TipSet) (reward.State, error) {
	st, err := sm.ParentState(ts)
	if err != nil {
		return nil, err
	}

	act, err := st.GetActor(reward.Address)
	if err != nil {
		return nil, err
	}

	actState, err := reward.Load(sm.cs.ActorStore(ctx), act)
	if err != nil {
		return nil, err
	}
	return actState, nil
}



func (sm *StateManager) VMSys() vm.SyscallBuilder {
	return sm.syscalls
}
