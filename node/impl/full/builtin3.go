package full

import (
	"github.com/filecoin-project/specs-actors/v3/actors/builtin"
	"github.com/ipfs/go-cid"
)

func BuiltinName3(code cid.Cid)  string{
	return builtin.ActorNameByCode(code)
}
