package stmgr

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/cron"

	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
	blockadt "github.com/filecoin-project/specs-actors/actors/util/adt"
	"github.com/ipfs/go-cid"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"
)

func GetRewardLastPerEpochReward(ctx context.Context, sm *StateManager, ts *types.TipSet) (abi.TokenAmount, error) {
	rewardstate, err := sm.GetRewardState(ctx,ts)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("loading miner actor: %w", err)
	}


	return rewardstate.ThisEpochReward()
}

func GetStatesInfo(ctx context.Context, sm *StateManager, ts *types.TipSet) (*api.ActorStates, error) {
	return nil, xerrors.Errorf("no impl")
}


func (sm *StateManager) LoadActorStateRaw(ctx context.Context, addr address.Address, out interface{}, st cid.Cid) (*types.Actor, error) {
	var a *types.Actor
	if err := sm.WithStateTree(st, sm.WithActor(addr, func(act *types.Actor) error {
		a = act
		return sm.WithActorState(ctx, out)(act)
	})); err != nil {
		return nil, err
	}

	return a, nil
}


func ComputeStateList(ctx context.Context, sm *StateManager, ts *types.TipSet) ( []*api.InvocResult, error) {
	if ts == nil {
		ts = sm.cs.GetHeaviestTipSet()
	}

	_, trace, err := sm.ExecutionTraceCompute(ctx, ts)
	if err != nil {
		return  nil, err
	}

	//for i := ts.Height(); i < ts.Height(); i++ {
	//	fmt.Println("ComputeStateListI",i)
	//	// handle state forks
	//	base, err = sm.handleStateForks(ctx, base, i, traceFuncCompute(&trace), ts)
	//	if err != nil {
	//		return  nil, xerrors.Errorf("error handling state forks: %w", err)
	//	}
	//
	//	// TODO: should we also run cron here?
	//}
	return  trace, nil
}


func (sm *StateManager) ExecutionTraceCompute(ctx context.Context, ts *types.TipSet) (cid.Cid, []*api.InvocResult, error) {
	var trace []*api.InvocResult
	//actor, err := a.StateManager.LoadActorTsk(ctx, msg.VMMessage().To, t)

	st, _, err := sm.computeTipSetStateCompute(ctx, ts, &InvocationTracer{trace: &trace})
	if err != nil {
		return cid.Undef, nil, err
	}

	return st, trace, nil
}


func traceFuncCompute(trace *[]*api.InvocResults) func(mcid cid.Cid, msg *types.Message, ret *vm.ApplyRet) error {
	return func(mcid cid.Cid, msg *types.Message, ret *vm.ApplyRet) error {

		ir := &api.InvocResults{
			Timestamp:      ret.Timestamp,
			MsgCid:         mcid,
			//Msg:            msg,
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



func (sm *StateManager) computeTipSetStateCompute(ctx context.Context, ts *types.TipSet, em ExecMonitor) (cid.Cid, cid.Cid, error) {
	ctx, span := trace.StartSpan(ctx, "computeTipSetState")
	defer span.End()

	blks := ts.Blocks()
	//for i := 0; i < len(blks); i++ {
	//	for j := i + 1; j < len(blks); j++ {
	//		if blks[i].Miner == blks[j].Miner {
	//			return cid.Undef, cid.Undef,
	//				xerrors.Errorf("duplicate miner in a tipset (%s %s)",
	//					blks[i].Miner, blks[j].Miner)
	//		}
	//	}
	//}

	var parentEpoch abi.ChainEpoch
	pstate := blks[0].ParentStateRoot
	if blks[0].Height > 0 {
		parent, err := sm.cs.GetBlock(blks[0].Parents[0])
		if err != nil {
			return cid.Undef, cid.Undef, xerrors.Errorf("getting parent block: %w", err)
		}

		parentEpoch = parent.Height
	}

	r := store.NewChainRand(sm.cs, ts.Cids())

	blkmsgs, err := sm.cs.BlockMsgsForTipset(ts)
	if err != nil {
		return cid.Undef, cid.Undef, xerrors.Errorf("getting block messages for tipset: %w", err)
	}

	baseFee := blks[0].ParentBaseFee

	return sm.ApplyBlocksCompute(ctx, parentEpoch, pstate, blkmsgs, blks[0].Height, r, em, baseFee, ts)
}



func (sm *StateManager) ApplyBlocksCompute(ctx context.Context, parentEpoch abi.ChainEpoch, pstate cid.Cid, bms []store.BlockMessages, epoch abi.ChainEpoch, r vm.Rand, em ExecMonitor, baseFee abi.TokenAmount, ts *types.TipSet) (cid.Cid, cid.Cid, error) {
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


	processedMsgs := make(map[cid.Cid]struct{})
	for _, b := range bms {
		//fmt.Println("firi",i,bcid,len(b.BlsMessages),len(b.SecpkMessages))
		penalty := types.NewInt(0)
		var gasReward = big.Zero()
		for _, cm := range append(b.BlsMessages, b.SecpkMessages...) {
			m := cm.VMMessage()
			if _, found := processedMsgs[m.Cid()]; found {
				continue
			}
			r, err := vmi.ApplyMessage(ctx, cm)
			if err != nil {
				return cid.Undef, cid.Undef, err
			}
			r.Timestamp = b.Timestamp
			r.Height = b.Height
			r.BlockId = b.BlockId

			//receipts = append(receipts, &r.MessageReceipt)
			gasReward = big.Add(gasReward, r.GasCosts.MinerTip)
			penalty = big.Add(penalty, r.GasCosts.MinerPenalty)

			if em != nil {
				if err := em.MessageApplied(ctx, ts, cm.Cid(), m, r, false); err != nil {
					return cid.Undef, cid.Undef, err
				}
			}
			processedMsgs[m.Cid()] = struct{}{}
		}


	}


	if err := runCron(epoch); err != nil {
		return cid.Cid{}, cid.Cid{}, err
	}


	// XXX: Is the height correct? Or should it be epoch-1?
	rectarr := blockadt.MakeEmptyArray(sm.cs.ActorStore(ctx))
	//for i, receipt := range receipts {
	//	if err := rectarr.Set(uint64(i), receipt); err != nil {
	//		return cid.Undef, cid.Undef, xerrors.Errorf("failed to build receipts amt: %w", err)
	//	}
	//}
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

