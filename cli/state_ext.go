package cli

import (
	"encoding/base64"
	"fmt"
	"github.com/filecoin-project/go-state-types/abi"
	"strconv"

	"github.com/filecoin-project/go-address"
	"github.com/urfave/cli/v2"
)

var StateDecodeReturnCmd = &cli.Command{
	Name:  "decode-return",
	Usage: "<actor code> <method> <para>",
	Action: func(cctx *cli.Context) error {
		api, closer, err := GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()

		ctx := ReqContext(cctx)
		actAddr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return err
		}
		method, err := strconv.ParseInt(cctx.Args().Get(1), 10, 64)
		if err != nil {
			return err
		}
		para, err := base64.StdEncoding.DecodeString(cctx.Args().Get(2))
		if err != nil {
			return err
		}
		res, err := api.StateDecodeReturn(ctx, actAddr, abi.MethodNum(method), para)
		if err != nil {
			return err
		}
		fmt.Printf("result:\n")
		fmt.Printf("%v\n", res)
		return nil
	},
}
