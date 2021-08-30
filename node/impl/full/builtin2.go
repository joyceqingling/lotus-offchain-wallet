package full

import (
	"github.com/filecoin-project/specs-actors/v2/actors/builtin"
	"github.com/ipfs/go-cid"
)

func BuiltinName2(code cid.Cid)  string{
	return builtin.ActorNameByCode(code)
}
