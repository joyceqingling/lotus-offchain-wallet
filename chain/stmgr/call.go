package stmgr

import (
	"context"
	"errors"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/ipfs/go-cid"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/vm"
)

var ErrExpensiveFork = errors.New("refusing explicit call due to state fork at epoch")

func (sm *StateManager) Call(ctx context.Context, msg *types.Message, ts *types.TipSet) (*api.InvocResult, error) {
	ctx, span := trace.StartSpan(ctx, "statemanager.Call")
	defer span.End()

	// If no tipset is provided, try to find one without a fork.
	if ts == nil {
		ts = sm.cs.GetHeaviestTipSet()

		// Search back till we find a height with no fork, or we reach the beginning.
		for ts.Height() > 0 && sm.hasExpensiveFork(ctx, ts.Height()-1) {
			var err error
			ts, err = sm.cs.GetTipSetFromKey(ts.Parents())
			if err != nil {
				return nil, xerrors.Errorf("failed to find a non-forking epoch: %w", err)
			}
		}
	}

	bstate := ts.ParentState()
	pts, err := sm.cs.LoadTipSet(ts.Parents())
	if err != nil {
		return nil, xerrors.Errorf("failed to load parent tipset: %w", err)
	}
	pheight := pts.Height()

	// If we have to run an expensive migration, and we're not at genesis,
	// return an error because the migration will take too long.
	//
	// We allow this at height 0 for at-genesis migrations (for testing).
	if pheight > 0 && sm.hasExpensiveFork(ctx, pheight) {
		return nil, ErrExpensiveFork
	}

	// Run the (not expensive) migration.
	bstate, err = sm.handleStateForks(ctx, bstate, pheight, nil, ts)
	if err != nil {
		return nil, fmt.Errorf("failed to handle fork: %w", err)
	}

	vmopt := &vm.VMOpts{
		StateBase:      bstate,
		Epoch:          pheight + 1,
		Rand:           store.NewChainRand(sm.cs, ts.Cids()),
		Bstore:         sm.cs.StateBlockstore(),
		Syscalls:       sm.syscalls,
		CircSupplyCalc: sm.GetVMCirculatingSupply,
		NtwkVersion:    sm.GetNtwkVersion,
		BaseFee:        types.NewInt(0),
		LookbackState:  LookbackStateGetterForTipset(sm, ts),
	}

	vmi, err := sm.newVM(ctx, vmopt)
	if err != nil {
		return nil, xerrors.Errorf("failed to set up vm: %w", err)
	}

	if msg.GasLimit == 0 {
		msg.GasLimit = build.BlockGasLimit
	}
	if msg.GasFeeCap == types.EmptyInt {
		msg.GasFeeCap = types.NewInt(0)
	}
	if msg.GasPremium == types.EmptyInt {
		msg.GasPremium = types.NewInt(0)
	}

	if msg.Value == types.EmptyInt {
		msg.Value = types.NewInt(0)
	}

	if span.IsRecordingEvents() {
		span.AddAttributes(
			trace.Int64Attribute("gas_limit", msg.GasLimit),
			trace.StringAttribute("gas_feecap", msg.GasFeeCap.String()),
			trace.StringAttribute("value", msg.Value.String()),
		)
	}

	fromActor, err := vmi.StateTree().GetActor(msg.From)
	if err != nil {
		return nil, xerrors.Errorf("call raw get actor: %s", err)
	}

	msg.Nonce = fromActor.Nonce

	// TODO: maybe just use the invoker directly?
	ret, err := vmi.ApplyImplicitMessage(ctx, msg)
	if err != nil {
		return nil, xerrors.Errorf("apply message failed: %w", err)
	}

	var errs string
	if ret.ActorErr != nil {
		errs = ret.ActorErr.Error()
		log.Warnf("chain call failed: %s", ret.ActorErr)
	}

	return &api.InvocResult{
		MsgCid:         msg.Cid(),
		Msg:            msg,
		MsgRct:         &ret.MessageReceipt,
		ExecutionTrace: ret.ExecutionTrace,
		Error:          errs,
		Duration:       ret.Duration,
	}, nil

}


func (sm *StateManager) CallExt(ctx context.Context, msg *types.Message, ts *types.TipSet) (*api.InvocResult, error) {
	ctx, span := trace.StartSpan(ctx, "statemanager.Call")
	defer span.End()

	// If no tipset is provided, try to find one without a fork.
	if ts == nil {
		ts = sm.cs.GetHeaviestTipSet()

		// Search back till we find a height with no fork, or we reach the beginning.
		for ts.Height() > 0 && sm.hasExpensiveFork(ctx, ts.Height()-1) {
			var err error
			ts, err = sm.cs.GetTipSetFromKey(ts.Parents())
			if err != nil {
				return nil, xerrors.Errorf("failed to find a non-forking epoch: %w", err)
			}
		}
	}

	bstate := ts.ParentState()
	bheight := ts.Height()

	// If we have to run an expensive migration, and we're not at genesis,
	// return an error because the migration will take too long.
	//
	// We allow this at height 0 for at-genesis migrations (for testing).
	if bheight-1 > 0 && sm.hasExpensiveFork(ctx, bheight-1) {
		return nil, ErrExpensiveFork
	}

	// Run the (not expensive) migration.
	bstate, err := sm.handleStateForks(ctx, bstate, bheight-1, nil, ts)
	if err != nil {
		return nil, fmt.Errorf("failed to handle fork: %w", err)
	}



	vmopt := &vm.VMOpts{
		StateBase:      bstate,
		Epoch:          bheight,
		Rand:           store.NewChainRand(sm.cs, ts.Cids()),
		Bstore:         sm.cs.StateBlockstore(),
		Syscalls:       sm.syscalls,
		CircSupplyCalc: sm.GetVMCirculatingSupply,
		NtwkVersion:    sm.GetNtwkVersion,
		BaseFee:        types.NewInt(0),
		LookbackState:  LookbackStateGetterForTipset(sm, ts),
	}

	vmi, err := sm.newVM(ctx, vmopt)
	if err != nil {
		return nil, xerrors.Errorf("failed to set up vm: %w", err)
	}

	if msg.VMMessage().GasLimit == 0 {
		msg.VMMessage().GasLimit = build.BlockGasLimit
	}
	if msg.VMMessage().GasFeeCap == types.EmptyInt {
		msg.VMMessage().GasFeeCap = types.NewInt(0)
	}
	if msg.VMMessage().GasPremium == types.EmptyInt {
		msg.VMMessage().GasPremium = types.NewInt(0)
	}

	if msg.VMMessage().Value == types.EmptyInt {
		msg.VMMessage().Value = types.NewInt(0)
	}

	if span.IsRecordingEvents() {
		span.AddAttributes(
			trace.Int64Attribute("gas_limit", msg.VMMessage().GasLimit),
			trace.StringAttribute("gas_feecap", msg.VMMessage().GasFeeCap.String()),
			trace.StringAttribute("value", msg.VMMessage().Value.String()),
		)
	}

	fromActor, err := vmi.StateTree().GetActor(msg.VMMessage().From)
	if err != nil {
		return nil, xerrors.Errorf("call raw get actor: %s", err)
	}

	msg.VMMessage().Nonce = fromActor.Nonce

	var msgApply types.ChainMsg
	fromKey, err := sm.ResolveToKeyAddress(ctx, msg.From, ts)

	switch fromKey.Protocol() {
	case address.BLS:
		msgApply = msg
	case address.SECP256K1:
		msgApply = &types.SignedMessage{
			Message: *msg,
			Signature: crypto.Signature{
				Type: crypto.SigTypeSecp256k1,
				Data: make([]byte, 65),
			},
		}

	}

	// TODO: maybe just use the invoker directly?
	ret, err := vmi.ApplyMessage(ctx, msgApply)
	if err != nil {
		return nil, xerrors.Errorf("apply message failed: %w", err)
	}

	var errs string
	if ret.ActorErr != nil {
		errs = ret.ActorErr.Error()
		log.Warnf("chain call failed: %s", ret.ActorErr)
	}

	return &api.InvocResult{
		GasCost:      MakeMsgGasCost(msg.VMMessage(), ret),
		MsgCid:         msg.Cid(),
		Msg:            msg.VMMessage(),
		MsgRct:         &ret.MessageReceipt,
		ExecutionTrace: ret.ExecutionTrace,
		Error:          errs,
		Duration:       ret.Duration,
	}, nil

}


func (sm *StateManager) CallWithGas(ctx context.Context, msg *types.Message, priorMsgs []types.ChainMsg, ts *types.TipSet) (*api.InvocResult, error) {
	ctx, span := trace.StartSpan(ctx, "statemanager.CallWithGas")
	defer span.End()

	if ts == nil {
		ts = sm.cs.GetHeaviestTipSet()

		// Search back till we find a height with no fork, or we reach the beginning.
		// We need the _previous_ height to have no fork, because we'll
		// run the fork logic in `sm.TipSetState`. We need the _current_
		// height to have no fork, because we'll run it inside this
		// function before executing the given message.
		for ts.Height() > 0 && (sm.hasExpensiveFork(ctx, ts.Height()) || sm.hasExpensiveFork(ctx, ts.Height()-1)) {
			var err error
			ts, err = sm.cs.GetTipSetFromKey(ts.Parents())
			if err != nil {
				return nil, xerrors.Errorf("failed to find a non-forking epoch: %w", err)
			}
		}
	}

	// When we're not at the genesis block, make sure we don't have an expensive migration.
	if ts.Height() > 0 && (sm.hasExpensiveFork(ctx, ts.Height()) || sm.hasExpensiveFork(ctx, ts.Height()-1)) {
		return nil, ErrExpensiveFork
	}

	state, _, err := sm.TipSetState(ctx, ts)
	if err != nil {
		return nil, xerrors.Errorf("computing tipset state: %w", err)
	}

	state, err = sm.handleStateForks(ctx, state, ts.Height(), nil, ts)
	if err != nil {
		return nil, fmt.Errorf("failed to handle fork: %w", err)
	}

	r := store.NewChainRand(sm.cs, ts.Cids())

	if span.IsRecordingEvents() {
		span.AddAttributes(
			trace.Int64Attribute("gas_limit", msg.GasLimit),
			trace.StringAttribute("gas_feecap", msg.GasFeeCap.String()),
			trace.StringAttribute("value", msg.Value.String()),
		)
	}

	vmopt := &vm.VMOpts{
		StateBase:      state,
		Epoch:          ts.Height() + 1,
		Rand:           r,
		Bstore:         sm.cs.StateBlockstore(),
		Syscalls:       sm.syscalls,
		CircSupplyCalc: sm.GetVMCirculatingSupply,
		NtwkVersion:    sm.GetNtwkVersion,
		BaseFee:        ts.Blocks()[0].ParentBaseFee,
		LookbackState:  LookbackStateGetterForTipset(sm, ts),
	}
	vmi, err := sm.newVM(ctx, vmopt)
	if err != nil {
		return nil, xerrors.Errorf("failed to set up vm: %w", err)
	}
	for i, m := range priorMsgs {
		_, err := vmi.ApplyMessage(ctx, m)
		if err != nil {
			return nil, xerrors.Errorf("applying prior message (%d, %s): %w", i, m.Cid(), err)
		}
	}

	fromActor, err := vmi.StateTree().GetActor(msg.From)
	if err != nil {
		return nil, xerrors.Errorf("call raw get actor: %s", err)
	}

	msg.Nonce = fromActor.Nonce

	fromKey, err := sm.ResolveToKeyAddress(ctx, msg.From, ts)
	if err != nil {
		return nil, xerrors.Errorf("could not resolve key: %w", err)
	}

	var msgApply types.ChainMsg

	switch fromKey.Protocol() {
	case address.BLS:
		msgApply = msg
	case address.SECP256K1:
		msgApply = &types.SignedMessage{
			Message: *msg,
			Signature: crypto.Signature{
				Type: crypto.SigTypeSecp256k1,
				Data: make([]byte, 65),
			},
		}

	}

	ret, err := vmi.ApplyMessage(ctx, msgApply)
	if err != nil {
		return nil, xerrors.Errorf("apply message failed: %w", err)
	}

	var errs string
	if ret.ActorErr != nil {
		errs = ret.ActorErr.Error()
	}

	return &api.InvocResult{
		MsgCid:         msg.Cid(),
		Msg:            msg,
		MsgRct:         &ret.MessageReceipt,
		GasCost:        MakeMsgGasCost(msg, ret),
		ExecutionTrace: ret.ExecutionTrace,
		Error:          errs,
		Duration:       ret.Duration,
	}, nil
}

var errHaltExecution = fmt.Errorf("halt")

func (sm *StateManager) Replay(ctx context.Context, ts *types.TipSet, mcid cid.Cid) (*types.Message, *vm.ApplyRet, error) {
	var finder messageFinder
	// message to find
	finder.mcid = mcid

	_, _, err := sm.computeTipSetState(ctx, ts, &finder)
	if err != nil && !xerrors.Is(err, errHaltExecution) {
		return nil, nil, xerrors.Errorf("unexpected error during execution: %w", err)
	}

	if finder.outr == nil {
		return nil, nil, xerrors.Errorf("given message not found in tipset")
	}

	return finder.outm, finder.outr, nil
}


//func (sm *StateManager) computeTipSetStateEx(ctx context.Context, ts *types.TipSet, cb ExecCallback) (cid.Cid, cid.Cid, error) {
//	ctx, span := trace.StartSpan(ctx, "computeTipSetState")
//	defer span.End()
//
//	blks := ts.Blocks()
//
//
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
//	return sm.ApplyMessages(ctx, parentEpoch, pstate, blkmsgs, blks[0].Height, r, cb, baseFee, ts,cid.Undef)
//}

//func (sm *StateManager) computeTipSetStateExs(ctx context.Context, ts *types.TipSet, cb ExecCallback,mid cid.Cid) (cid.Cid, cid.Cid, error) {
//	ctx, span := trace.StartSpan(ctx, "computeTipSetState")
//	defer span.End()
//
//	blks := ts.Blocks()
//
//	//for i := 0; i < len(blks); i++ {
//	//	for j := i + 1; j < len(blks); j++ {
//	//		if blks[i].Miner == blks[j].Miner {
//	//			return cid.Undef, cid.Undef,
//	//				xerrors.Errorf("duplicate miner in a tipset (%s %s)",
//	//					blks[i].Miner, blks[j].Miner)
//	//		}
//	//	}
//	//}
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
//	return sm.ApplyMessages(ctx, parentEpoch, pstate, blkmsgs, blks[0].Height, r, cb, baseFee, ts,mid)
//}


//func (sm *StateManager) ReplayEx(ctx context.Context, ts *types.TipSet) (*types.Message, []*api.InvocResults, error) {
//	var outm *types.Message
//	var outr *vm.ApplyRet
//	var trace []*api.InvocResults
//	_, _, err := sm.computeTipSetStateEx(ctx, ts,  traceFuncCompute(&trace))
//	if err != nil && err != errHaltExecution {
//		return nil, nil, xerrors.Errorf("unexpected error during execution: %w", err)
//	}
//
//	if outr == nil {
//		return nil, nil, xerrors.Errorf("given message not found in tipset")
//	}
//
//	return outm, trace, nil
//}


//func (sm *StateManager) Replays(ctx context.Context, ts *types.TipSet, mcid[] cid.Cid) ([]*types.Message, []*vm.ApplyRet, error) {
//	var outm []*types.Message
//	var outr []*vm.ApplyRet
//
//	_, _, err := sm.computeTipSetState(ctx, ts, func(c cid.Cid, m *types.Message, ret *vm.ApplyRet) error {
//		for _, c2 := range mcid {
//			if c == c2{
//				outm =append(outm, m)
//				outr = append(outr,ret )
//				return errHaltExecution
//			}
//		}
//
//		return nil
//	})
//	if err != nil && !xerrors.Is(err, errHaltExecution) {
//		return nil, nil, xerrors.Errorf("unexpected error during execution: %w", err)
//	}
//
//	if outr == nil {
//		return nil, nil, xerrors.Errorf("given message not found in tipset")
//	}
//
//	return outm, outr, nil
//}
