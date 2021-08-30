package impl

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

func (f FullNodeAPI) StateGetActorsInfo(ctx context.Context, tsk types.TipSetKey) (*api.ActorStates, error) {
	return f.StateAPI.StateGetActorsInfo(ctx, tsk)
}

func (f FullNodeAPI) StateGetRewardLastPerEpochReward(ctx context.Context, tsk types.TipSetKey) (abi.TokenAmount, error) {
	return f.StateAPI.StateGetRewardLastPerEpochReward(ctx, tsk)
}

func (f FullNodeAPI) StateMinerSectorNumbers(ctx context.Context, a address.Address, key types.TipSetKey) (num []uint64, err error) {

	s, err := f.StateAPI.StateMinerSectors(ctx, a, nil, key)
	if err != nil {
		return nil, err
	}

	for _, i := range s {
		num = append(num, uint64(i.SectorNumber))
	}

	return
}

func (f FullNodeAPI) StatePreCommittedSectors(ctx context.Context, a address.Address, key types.TipSetKey) ([]uint64, error) {
	return f.StateAPI.StatePreCommittedSectors(ctx, a, key)
}
