package full

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	"golang.org/x/xerrors"
	"sort"
)

func (a *StateAPI) StateGetActorsInfo(ctx context.Context, tsk types.TipSetKey) (*api.ActorStates, error) {
	ts, err := a.Chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}

	minerMarketBalance, err := a.StateMarketParticipants(ctx, tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading StateMarketParticipants %s: %w", tsk, err)
	}
	var minerMarketBalanceInfo []api.MarketBalanceInfo
	for k, v := range minerMarketBalance {
		minerMarketBalanceInfo = append(minerMarketBalanceInfo, api.MarketBalanceInfo{
			Miner:  k,
			Escrow: v.Escrow,
			Locked: v.Locked,
		})
	}
	sort.Slice(minerMarketBalanceInfo, func(i, j int) bool {
		return big.Cmp(minerMarketBalanceInfo[i].Escrow, minerMarketBalanceInfo[j].Escrow) > 0
	})
	as, err := stmgr.GetStatesInfo(ctx, a.StateManager, ts)
	if err != nil {
		return nil, xerrors.Errorf("loading GetStatesInfo %s: %w", tsk, err)
	}
	as.StorageMarket.MinerMarketBalance = minerMarketBalanceInfo
	return as, nil
}

func (a *StateAPI) StateGetRewardLastPerEpochReward(ctx context.Context, tsk types.TipSetKey) (abi.TokenAmount, error) {
	ts, err := a.Chain.GetTipSetFromKey(tsk)
	if err != nil {
		return types.EmptyInt, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	return stmgr.GetRewardLastPerEpochReward(ctx, a.StateManager, ts)
}

func (a *StateAPI) StatePreCommittedSectors(ctx context.Context, maddr address.Address, tsk types.TipSetKey) ([]uint64, error) {
	return nil, xerrors.Errorf("no impl")
}
