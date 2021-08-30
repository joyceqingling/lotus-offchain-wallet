package api

import (
	"bytes"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/exitcode"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/specs-actors/actors/builtin/verifreg"
	block "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
	"io"
	"time"
)

type ActorStates struct {
	StorageMarket         MarketState
	StoragePower          StoragePowerState
	Reward                RewardState
	VerifiedRegistryState VerifiedRegistryState
}

type StorageMinerInfo struct {
	Escrow string
	Locked string
}

type MarketBalanceInfo struct {
	Miner  string
	Escrow verifreg.DataCap
	Locked verifreg.DataCap
}

type MarketState struct {
	MinerMarketBalance []MarketBalanceInfo
	NextID             abi.DealID
	// Total Client Collateral that is locked -> unlocked when deal is terminated
	TotalClientLockedCollateral abi.TokenAmount
	// Total Provider Collateral that is locked -> unlocked when deal is terminated
	TotalProviderLockedCollateral abi.TokenAmount
	// Total storage fee that is locked in escrow -> unlocked when payments are made
	TotalClientStorageFee abi.TokenAmount
}
type StoragePowerState struct {
	TotalRawBytePower abi.StoragePower
	// TotalBytesCommitted includes claims from miners below min power threshold
	TotalBytesCommitted  abi.StoragePower
	TotalQualityAdjPower abi.StoragePower
	// TotalQABytesCommitted includes claims from miners below min power threshold
	TotalQABytesCommitted abi.StoragePower
	TotalPledgeCollateral abi.TokenAmount

	// These fields are set once per epoch in the previous cron tick and used
	// for consistent values across a single epoch's state transition.
	ThisEpochRawBytePower     abi.StoragePower
	ThisEpochQualityAdjPower  abi.StoragePower
	ThisEpochPledgeCollateral abi.TokenAmount

	MinerCount int64
	// Number of miners having proven the minimum consensus power.
	MinerAboveMinPowerCount int64
	InitialPledgeFor32GiB   abi.TokenAmount

	Claims []Claim
}

type Claim struct {
	Miner           address.Address
	RawBytePower    abi.StoragePower
	QualityAdjPower abi.StoragePower
}

type VerifiedRegistryState struct {
	VerifiedClients map[string]verifreg.DataCap
	Verifiers       map[string]verifreg.DataCap
}
type RewardState struct {
	// CumsumBaseline is a target CumsumRealized needs to reach for EffectiveNetworkTime to increase
	// CumsumBaseline and CumsumRealized are expressed in byte-epochs.
	CumsumBaseline abi.StoragePower

	// CumsumRealized is cumulative sum of network power capped by BalinePower(epoch)
	CumsumRealized abi.StoragePower

	// EffectiveNetworkTime is ceiling of real effective network time `theta` based on
	// CumsumBaselinePower(theta) == CumsumRealizedPower
	// Theta captures the notion of how much the network has progressed in its baseline
	// and in advancing network time.
	EffectiveNetworkTime abi.ChainEpoch

	// The reward to be paid in per WinCount to block producers.
	// The actual reward total paid out depends on the number of winners in any round.
	// This value is recomputed every non-null epoch and used in the next non-null epoch.
	ThisEpochReward   abi.TokenAmount
	CirculatingSupply abi.TokenAmount

	// The baseline power the network is targeting at st.Epoch
	ThisEpochBaselinePower abi.StoragePower

	// Epoch tracks for which epoch the Reward was computed
	Epoch abi.ChainEpoch
}

type NBlockMessages struct {
	BlsMessages   []*NMessage
	SecpkMessages []*NMessage

	//Cids []cid.Cid
}


type StateMinerDeadlinesByMinerInfo struct {
	Sectors uint64
	// Subset of sectors detected/declared faulty and not yet recovered (excl. from PoSt).
	// Faults ∩ Terminated = ∅
	Faults uint64
	// Subset of faulty sectors expected to recover on next PoSt
	// Recoveries ∩ Terminated = ∅
	Recoveries uint64
	// Subset of sectors terminated but not yet removed from partition (excl. from PoSt)
	Terminated uint64
	// Maps epochs sectors that expire in or before that epoch.
	// An expiration may be an "on-time" scheduled expiration, or early "faulty" expiration.
	// Keys are quantized to last-in-deadline epochs.
	ExpirationsEpochs cid.Cid // AMT[ChainEpoch]ExpirationSet
	// Subset of terminated that were before their committed expiration epoch, by termination epoch.
	// Termination fees have not yet been calculated or paid and associated deals have not yet been
	// canceled but effective power has already been adjusted.
	// Not quantized.
	EarlyTerminated cid.Cid // AMT[ChainEpoch]BitField

	// Power of not-yet-terminated sectors (incl faulty).
	LivePower uint64
	// Power of currently-faulty sectors. FaultyPower <= LivePower.
	FaultyPower uint64
	// Power of expected-to-recover sectors. RecoveringPower <= FaultyPower.
	RecoveringPower uint64

	Index        uint64

	PostSubmissions bitfield.BitField

	// Partitions with sectors that terminated early.
	EarlyTerminations bitfield.BitField
	Wpost int


}


type NMessage struct {
	Timestamp uint64
	Version uint64

	To   address.Address
	From address.Address

	Nonce uint64

	Value abi.TokenAmount

	GasLimit   int64
	GasFeeCap  abi.TokenAmount
	GasPremium abi.TokenAmount

	Method abi.MethodNum
	Params string

	CID cid.Cid

	BlockCid cid.Cid


	CodeMethod string

	MsgRct         *types.MessageReceipt
	GasCost        MsgGasCost
	ExecutionTrace types.ExecutionTrace
	Error          string
	Duration       time.Duration
	Paramss []byte
}


type SMessage struct {
	Version uint64
	Height abi.ChainEpoch
	Tipset  []cid.Cid
	To   address.Address
	From address.Address

	Nonce uint64

	Params string

	Value abi.TokenAmount

	GasLimit   int64
	GasFeeCap  abi.TokenAmount
	GasPremium abi.TokenAmount

	Method abi.MethodNum

	CID cid.Cid



	CodeMethod string

	ExitCode exitcode.ExitCode

	Return string
}


type StateGetMessageDealsByHeightIntervalResp struct {
	Msg *NMessage
    Deal []*MarketDeal
}




type Blocks struct {
	Miner       address.Address
	Cid         string
	Reward      abi.TokenAmount
	Timestamp uint64
	Height abi.ChainEpoch
}

func (m *NMessage) Cid() cid.Cid {
	b, err := m.ToStorageBlock()
	if err != nil {
		panic(fmt.Sprintf("failed to marshal message: %s", err)) // I think this is maybe sketchy, what happens if we try to serialize a message with an undefined address in it?
	}

	return b.Cid()
}

func (m *NMessage) ToStorageBlock() (block.Block, error) {
	data, err := m.Serialize()
	if err != nil {
		return nil, err
	}

	c, err := abi.CidBuilder.Sum(data)
	if err != nil {
		return nil, err
	}

	return block.NewBlockWithCid(data, c)
}


func (m *NMessage) Serialize() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := m.MarshalCBOR(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var lengthBufMessage = []byte{138}

func (t *NMessage) MarshalCBOR(w io.Writer) error {
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
	if _, err := w.Write(lengthBufMessage); err != nil {
		return err
	}

	scratch := make([]byte, 9)

	// t.Version (uint64) (uint64)

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.Version)); err != nil {
		return err
	}

	// t.To (address.Address) (struct)
	if err := t.To.MarshalCBOR(w); err != nil {
		return err
	}

	// t.From (address.Address) (struct)
	if err := t.From.MarshalCBOR(w); err != nil {
		return err
	}

	// t.Nonce (uint64) (uint64)

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.Nonce)); err != nil {
		return err
	}

	// t.Value (big.Int) (struct)
	if err := t.Value.MarshalCBOR(w); err != nil {
		return err
	}

	// t.GasLimit (int64) (int64)
	if t.GasLimit >= 0 {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.GasLimit)); err != nil {
			return err
		}
	} else {
		if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajNegativeInt, uint64(-t.GasLimit-1)); err != nil {
			return err
		}
	}

	// t.GasFeeCap (big.Int) (struct)
	if err := t.GasFeeCap.MarshalCBOR(w); err != nil {
		return err
	}

	// t.GasPremium (big.Int) (struct)
	if err := t.GasPremium.MarshalCBOR(w); err != nil {
		return err
	}

	// t.Method (abi.MethodNum) (uint64)

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajUnsignedInt, uint64(t.Method)); err != nil {
		return err
	}

	// t.Params ([]uint8) (slice)
	if len(t.Params) > cbg.ByteArrayMaxLen {
		return xerrors.Errorf("Byte array in field t.Params was too long")
	}

	if err := cbg.WriteMajorTypeHeaderBuf(scratch, w, cbg.MajByteString, uint64(len(t.Params))); err != nil {
		return err
	}


	return nil
}



