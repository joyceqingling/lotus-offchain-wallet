package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api/v0api"
	"github.com/filecoin-project/lotus/chain/actors"
	miner2 "github.com/filecoin-project/specs-actors/v2/actors/builtin/miner"
	"github.com/skip2/go-qrcode"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gbrlsnchs/jwt/v3"
	"github.com/gorilla/mux"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	ledgerwallet "github.com/filecoin-project/lotus/chain/wallet/ledger"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	"github.com/filecoin-project/lotus/metrics"
	"github.com/filecoin-project/lotus/node/modules"
	"github.com/filecoin-project/lotus/node/repo"
)

var log = logging.Logger("main")

const FlagWalletRepo = "wallet-repo"

type jwtPayload struct {
	Allow []auth.Permission
}

func main() {
	lotuslog.SetupLogLevels()

	local := []*cli.Command{
		runCmd,
		getApiKeyCmd,
		walletSign,
		getWalletDefault,
		walletList,
		walletSend,
		walletImport,
	}

	app := &cli.App{
		Name:    "lotus-wallet",
		Usage:   "Basic external wallet",
		Version: build.UserVersion(),
		Description: `
lotus-wallet provides a remote wallet service for lotus.

To configure your lotus node to use a remote wallet:
* Run 'lotus-wallet get-api-key' to generate API key
* Start lotus-wallet using 'lotus-wallet run' (see --help for additional flags)
* Edit lotus config (~/.lotus/config.toml)
  * Find the '[Wallet]' section
  * Set 'RemoteBackend' to '[api key]:http://[wallet ip]:[wallet port]'
    (the default port is 1777)
* Start (or restart) the lotus daemon`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagWalletRepo,
				EnvVars: []string{"WALLET_PATH"},
				Value:   "~/.lotuswallet", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"LOTUS_PATH"},
				Hidden:  true,
				Value:   "~/.lotus",
			},
		},

		Commands: local,
	}
	app.Setup()

	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		//return
	}
}

var getApiKeyCmd = &cli.Command{
	Name:  "get-api-key",
	Usage: "Generate API Key",
	Action: func(cctx *cli.Context) error {
		lr, ks, err := openRepo(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() // nolint

		p := jwtPayload{
			Allow: []auth.Permission{api.PermAdmin},
		}

		authKey, err := modules.APISecret(ks, lr)
		if err != nil {
			return xerrors.Errorf("setting up api secret: %w", err)
		}

		k, err := jwt.Sign(&p, (*jwt.HMACSHA)(authKey))
		if err != nil {
			return xerrors.Errorf("jwt sign: %w", err)
		}

		fmt.Println(string(k))
		return nil
	},
}

var walletList = &cli.Command{
	Name:      "list",
	Usage:     "./lotus-wallet list ",
	ArgsUsage: "<nil>",
	Action: func(cctx *cli.Context) error {

		log.Info("Starting lotus list ")

		ctx := context.Background()

		lr, ks, err := openRepo(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() // nolint

		api, err := wallet.NewWallet(ks)
		if err != nil {
			return err
		}

		list, err := api.WalletList(ctx)

		if err != nil {
			return err
		}

		for _, a := range list {
			fmt.Println(a)
		}


		return nil
	},
}


var walletSign = &cli.Command{
	Name:      "sign",
	Usage:     "walletSign",
	ArgsUsage: "<nil>",
	Action: func(cctx *cli.Context) error {



		lr, ks, err := openRepo(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() // nolint

		apis, err := wallet.NewWallet(ks)
		if err != nil {
			return err
		}

		ctx := context.Background()


		if !cctx.Args().Present() || cctx.NArg() != 2 {
			return fmt.Errorf("must specify signing address and message to sign")
		}

		first := cctx.Args().First()

		fdata, err := ioutil.ReadFile(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		var str string

		if first == "0"{
			log.Info("Starting  WalletSend sign")

			var ws WalletSendReq

			if err := json.Unmarshal(fdata, &ws); err != nil {
				return err
			}


			//组装message
			msgResp := new(types.SignedMessage)


			newFromString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("newFromString not found 1")
			}
			newToString, err := address.NewFromString(ws.To)

			if err != nil {
				return fmt.Errorf("newFromString not found 2")
			}
			msgResp.Message.From = newFromString
			msgResp.Message.To = newToString

			//fmt.Printf("value is %v ",ws.Value)
			value, err := types.ParseFIL(ws.Value)
			if err != nil {
				return fmt.Errorf("failed to parse value amount: %w", err)
			}
			msgResp.Message.Value = abi.TokenAmount(value)


			msgResp.Message.Method = abi.MethodNum(0)


			if ws.GasFeeCap != ""{
				val, err := types.ParseFIL(ws.GasFeeCap)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("2282685400 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}

			if ws.GasPremium != ""{
				val, err := types.ParseFIL(ws.GasPremium)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("100000 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}

			if ws.GasLimit != 0{

				msgResp.Message.GasLimit = ws.GasLimit

			}else {
				msgResp.Message.GasLimit = 601835

			}

			msgResp.Message.Nonce = ws.Nonce


			//组装签名

			addressString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("fromString must params is null")
			}

			mb := msgResp.Message.Cid()


			//fmt.Printf("mbCid: %v\n", mb)

			sig, err := apis.WalletSign(ctx, addressString, mb.Bytes(), api.MsgMeta{
				Type:  api.MTChainMsg,
			})


			if err != nil {
				return xerrors.Errorf("failed to sign message: %w", err)
			}

			msgResp.Signature = *sig

			serialize, err := msgResp.Serialize()


			str = hex.EncodeToString(serialize)

		}else if first == "6"{
			log.Info("Starting  Withdraw sign")

			var ws WithdrawReq

			if err := json.Unmarshal(fdata, &ws); err != nil {
				return err
			}


			//组装message
			msgResp := new(types.SignedMessage)


			newFromString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("newFromString not found 1")
			}
			newToString, err := address.NewFromString(ws.To)

			if err != nil {
				return fmt.Errorf("newFromString not found 2")
			}
			msgResp.Message.From = newFromString
			msgResp.Message.To = newToString

			//fmt.Printf("value is %v ",ws.Value)

			msgResp.Message.Value = types.NewInt(0)


			msgResp.Message.Method = abi.MethodNum(16)


			if ws.GasFeeCap != ""{
				val, err := types.ParseFIL(ws.GasFeeCap)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("2282685400 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}

			if ws.GasPremium != ""{
				val, err := types.ParseFIL(ws.GasPremium)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("100000 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}

			if ws.GasLimit != 0{

				msgResp.Message.GasLimit = ws.GasLimit

			}else {
				msgResp.Message.GasLimit = 601835

			}

			msgResp.Message.Nonce = ws.Nonce

			amount, err := types.ParseFIL(ws.AmountRequested)
			if err != nil {
				return fmt.Errorf("failed to parse AmountRequested amount: %w", err)
			}

			params, err := actors.SerializeParams(&miner2.WithdrawBalanceParams{
				AmountRequested: abi.TokenAmount(amount), // Default to attempting to withdraw all the extra funds in the miner actor
			})
			if err != nil {
				return err
			}
			msgResp.Message.Params = params


			//组装签名

			addressString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("fromString must params is null")
			}

			mb := msgResp.Message.Cid()


			//fmt.Printf("mbCid: %v\n", mb)

			sig, err := apis.WalletSign(ctx, addressString, mb.Bytes(), api.MsgMeta{
				Type:  api.MTChainMsg,
			})


			if err != nil {
				return xerrors.Errorf("failed to sign message: %w", err)
			}

			msgResp.Signature = *sig

			serialize, err := msgResp.Serialize()


			str = hex.EncodeToString(serialize)



		}else if first == "23"{

			log.Info("Starting  ChangeOwnerAddress sign")

			var ws ChangeOwnerAddressReq

			if err := json.Unmarshal(fdata, &ws); err != nil {
				return err
			}


			//组装message
			msgResp := new(types.SignedMessage)


			newFromString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("newFromString not found 1")
			}
			newToString, err := address.NewFromString(ws.To)

			if err != nil {
				return fmt.Errorf("newFromString not found 2")
			}
			msgResp.Message.From = newFromString
			msgResp.Message.To = newToString

			//fmt.Printf("value is %v ",ws.Value)

			msgResp.Message.Value = types.NewInt(0)


			msgResp.Message.Method = abi.MethodNum(23)


			if ws.GasFeeCap != ""{
				val, err := types.ParseFIL(ws.GasFeeCap)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("2282685400 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}

			if ws.GasPremium != ""{
				val, err := types.ParseFIL(ws.GasPremium)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("100000 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}

			if ws.GasLimit != 0{

				msgResp.Message.GasLimit = ws.GasLimit

			}else {
				msgResp.Message.GasLimit = 601835

			}

			msgResp.Message.Nonce = ws.Nonce

			fromString, err := address.NewFromString(ws.NewAddrId)

			if err != nil {
				return xerrors.Errorf("serializing NewFromString: %w", err)
			}
			//fmt.Printf("NewAddrId: %v\n", fromString)
			//fmt.Printf("NewAddrId: %v\n", fromString.String())

			params, err := actors.SerializeParams(&fromString)
			if err != nil {
				return xerrors.Errorf("serializing params: %w", err)
			}

			msgResp.Message.Params = params


			//组装签名

			addressString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("fromString must params is null")
			}

			mb := msgResp.Message.Cid()


			//sfmt.Printf("mbCid: %v\n", mb)

			sig, err := apis.WalletSign(ctx, addressString, mb.Bytes(), api.MsgMeta{
				Type:  api.MTChainMsg,
			})


			if err != nil {
				return xerrors.Errorf("failed to sign message: %w", err)
			}

			msgResp.Signature = *sig

			serialize, err := msgResp.Serialize()


			str = hex.EncodeToString(serialize)


		}else if first == "3"{

			log.Info("Starting  ChangeWorkerAddress sign")

			var ws ChangeWorkerAddressReq

			if err := json.Unmarshal(fdata, &ws); err != nil {
				return err
			}


			//组装message
			msgResp := new(types.SignedMessage)


			newFromString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("newFromString not found 1")
			}
			newToString, err := address.NewFromString(ws.To)

			if err != nil {
				return fmt.Errorf("newFromString not found 2")
			}
			msgResp.Message.From = newFromString
			msgResp.Message.To = newToString

			//fmt.Printf("value is %v ",ws.Value)

			msgResp.Message.Value = types.NewInt(0)


			msgResp.Message.Method = abi.MethodNum(3)


			if ws.GasFeeCap != ""{
				val, err := types.ParseFIL(ws.GasFeeCap)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("2282685400 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasFeeCap = abi.TokenAmount(val)

			}

			if ws.GasPremium != ""{
				val, err := types.ParseFIL(ws.GasPremium)
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}else {
				val, err := types.ParseFIL("100000 attofil")
				if err != nil {
					return fmt.Errorf("failed to parse feecap amount: %w", err)
				}
				msgResp.Message.GasPremium = abi.TokenAmount(val)

			}

			if ws.GasLimit != 0{

				msgResp.Message.GasLimit = ws.GasLimit

			}else {
				msgResp.Message.GasLimit = 601835

			}

			msgResp.Message.Nonce = ws.Nonce

			newWorker, err := address.NewFromString(ws.NewWorker)

			if err != nil {
				return fmt.Errorf("failed to parse NewWorker: %w", err)
			}

			//fmt.Printf("NewControlAddrs: %v\n", ws.NewControlAddrs)

			cwp := &miner2.ChangeWorkerAddressParams{
				NewWorker:       newWorker,
				NewControlAddrs: ws.NewControlAddrs,
			}


			params, err := actors.SerializeParams(cwp)
			if err != nil {
				return xerrors.Errorf("serializing params: %w", err)
			}



			msgResp.Message.Params = params


			//组装签名

			addressString, err := address.NewFromString(ws.From)

			if err != nil {
				return fmt.Errorf("fromString must params is null")
			}

			mb := msgResp.Message.Cid()


			//fmt.Printf("mbCid: %v\n", mb)

			sig, err := apis.WalletSign(ctx, addressString, mb.Bytes(), api.MsgMeta{
				Type:  api.MTChainMsg,
			})


			if err != nil {
				return xerrors.Errorf("failed to sign message: %w", err)
			}

			msgResp.Signature = *sig

			serialize, err := msgResp.Serialize()


			str = hex.EncodeToString(serialize)


		}



		path := lr.Path()



		now := time.Now().Unix()

		string := strconv.FormatInt(now,10)

		image := path+"/offline_qrcode_"+string+".png"

		qrcode.WriteFile(str, qrcode.Medium, 256, image)

		fmt.Printf("Qrcode: %v\n", image)

		fmt.Printf("Sign result: %v\n", str)




		return nil
	},
}


var walletSend = &cli.Command{
	Name:      "send",
	Usage:     "walletSend",
	ArgsUsage: "<nil>",
	Action: func(cctx *cli.Context) error {

		//log.Info("Starting lotus wallet sign")
		//
		//ctx := context.Background()
		//
		//api, closer, err := lcli.GetFullNodeAPIV1(cctx)
		//if err != nil {
		//	return err
		//}
		//defer closer()
		//
		//
		//
		//fdata, err := ioutil.ReadFile(cctx.Args().First())
		//if err != nil {
		//	return err
		//}
		//
		//var ws WalletSendReq
		//
		//msg := new(types.SignedMessage)
		//
		//msg.ToStorageBlock()
		//
		//
		//
		//if err := json.Unmarshal(fdata, &ws); err != nil {
		//	return err
		//}
		//
		//marshal, err := json.Marshal(ws)
		//
		//if err != nil {
		//	return err
		//}
		//
		//fmt.Printf("walletSendReq is %v ",string(marshal))
		//
		//
		//
		//fromString, err := address.NewFromString(ws.Address)
		//
		//if err != nil {
		//	return fmt.Errorf("fromString must params is null")
		//}
		//
		//
		//
		//newFromString, err := address.NewFromString(ws.From)
		//
		//if err != nil {
		//	return fmt.Errorf("newFromString not found 1")
		//}
		//newToString, err := address.NewFromString(ws.To)
		//
		//if err != nil {
		//	return fmt.Errorf("newFromString not found 2")
		//}
		//msg.Message.From = newFromString
		//msg.Message.To = newToString
		//
		//fmt.Printf("value is %v ",ws.Value)
		//value, err := types.ParseFIL(ws.Value)
		//if err != nil {
		//	return fmt.Errorf("failed to parse value amount: %w", err)
		//}
		//msg.Message.Value = abi.TokenAmount(value)
		//msg.Message.Method = abi.MethodNum(0)
		//
		//sigBytes, err := hex.DecodeString(ws.Signature)
		//
		//if err != nil {
		//	return err
		//}
		//
		//var sig crypto.Signature
		//if err := sig.UnmarshalBinary(sigBytes); err != nil {
		//	return err
		//}
		//nonce, err := api.MpoolGetNonce(ctx, fromString)
		//
		//if err != nil {
		//	return fmt.Errorf("failed to get nonce: %w", err)
		//}
		//
		//
		//msg.Message.Nonce = nonce+1
		//
		//
		//fmt.Printf("sig.Type is %v ",sig.Type)
		//fmt.Printf("sig.Data is %v ",string(sig.Data))
		//
		//
		//msg.Signature = sig
		//
		//gasedMsg, err := api.GasEstimateMessageGas(ctx, &msg.Message, nil, types.EmptyTSK)
		//if err != nil {
		//	return fmt.Errorf("estimating gas: %w", err)
		//}
		//
		//msg.Message.GasFeeCap = gasedMsg.GasFeeCap
		//msg.Message.GasLimit = gasedMsg.GasLimit
		//msg.Message.GasPremium = gasedMsg.GasPremium
		////组装message 计算gas
		//if ws.Feecap != ""{
		//	val, err := types.ParseFIL(ws.Feecap)
		//	if err != nil {
		//		return fmt.Errorf("failed to parse feecap amount: %w", err)
		//	}
		//	msg.Message.GasFeeCap = abi.TokenAmount(val)
		//
		//
		//}
		//
		//mid, err := api.MpoolPush(ctx, msg)
		//if err != nil {
		//	return fmt.Errorf("failed to MpoolPush: %w", err)
		//}
		//
		//fmt.Println(mid)


		return nil
	},
}


var walletImport = &cli.Command{
	Name:      "import",
	Usage:     "import keys",
	ArgsUsage: "[<path> (optional, will read from stdin if omitted)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Usage: "specify input format for key",
			Value: "hex-lotus",
		},
		&cli.BoolFlag{
			Name:  "as-default",
			Usage: "import the given key as your new default key",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()

		lr, ks, err := openRepo(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() // nolint

		api, err := wallet.NewWallet(ks)
		if err != nil {
			return err
		}

		//ctx := ReqContext(cctx)

		var inpdata []byte
		if !cctx.Args().Present() || cctx.Args().First() == "-" {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Enter private key: ")
			indata, err := reader.ReadBytes('\n')
			if err != nil {
				return err
			}
			inpdata = indata

		} else {
			fdata, err := ioutil.ReadFile(cctx.Args().First())
			if err != nil {
				return err
			}
			inpdata = fdata
		}

		var ki types.KeyInfo
		switch cctx.String("format") {
		case "hex-lotus":
			data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
			if err != nil {
				return err
			}

			if err := json.Unmarshal(data, &ki); err != nil {
				return err
			}
		case "json-lotus":
			if err := json.Unmarshal(inpdata, &ki); err != nil {
				return err
			}
		case "gfc-json":
			var f struct {
				KeyInfo []struct {
					PrivateKey []byte
					SigType    int
				}
			}
			if err := json.Unmarshal(inpdata, &f); err != nil {
				return xerrors.Errorf("failed to parse go-filecoin key: %s", err)
			}

			gk := f.KeyInfo[0]
			ki.PrivateKey = gk.PrivateKey
			switch gk.SigType {
			case 1:
				ki.Type = types.KTSecp256k1
			case 2:
				ki.Type = types.KTBLS
			default:
				return fmt.Errorf("unrecognized key type: %d", gk.SigType)
			}
		default:
			return fmt.Errorf("unrecognized format: %s", cctx.String("format"))
		}

		addr, err := api.WalletImport(ctx, &ki)
		if err != nil {
			return err
		}

		//if cctx.Bool("as-default") {
		//	if err := api.WalletSetDefault(ctx, addr); err != nil {
		//		return fmt.Errorf("failed to set default key: %w", err)
		//	}
		//}

		fmt.Printf("imported key %s successfully!\n", addr)
		return nil
	},
}

type WalletSignReq struct {
	Type       string
	Data string
}

type WalletSendReq struct {
	From          string
	To            string
	Nonce         uint64
  	Value         string
	GasFeeCap        string
	GasLimit        int64
	GasPremium        string
	Method          int
}

type WithdrawReq struct {
	From          string
	To            string
	Nonce         uint64
	Value         string
	GasFeeCap        string
	GasLimit        int64
	GasPremium        string
	Method          int
	AmountRequested string
}

type ChangeOwnerAddressReq struct {
	From          string
	To            string
	Nonce         uint64
	Value         string
	GasFeeCap        string
	GasLimit        int64
	GasPremium        string
	Method          int
	NewAddrId string
}

type ChangeWorkerAddressReq struct {
	From          string
	To            string
	Nonce         uint64
	Value         string
	GasFeeCap        string
	GasLimit        int64
	GasPremium        string
	Method          int
	NewWorker string
	NewControlAddrs []address.Address
}



var getWalletDefault = &cli.Command{
	Name:      "get-default",
	Usage:     "sign a message",
	ArgsUsage: "<signing address> <hexMessage>",
	Action: func(cctx *cli.Context) error {

		log.Info("Starting lotus wallet sign")

		//ctx := lcli.ReqContext(cctx)
		//ctx, cancel := context.WithCancel(ctx)
		//defer cancel()
		//ctx := context.Background();

		// Register all metric views
		if err := view.Register(
			metrics.DefaultViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}

		lr, ks, err := openRepo(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() // nolint

		lw, err := wallet.NewWallet(ks)
		if err != nil {
			return err
		}

		getDefault, err := lw.GetDefault()

		fmt.Println(getDefault)


		return nil
	},
}


var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start lotus wallet",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "listen",
			Usage: "host address and port the wallet api will listen on",
			Value: "0.0.0.0:1777",
		},
		&cli.BoolFlag{
			Name:  "ledger",
			Usage: "use a ledger device instead of an on-disk wallet",
		},
		&cli.BoolFlag{
			Name:  "interactive",
			Usage: "prompt before performing actions (DO NOT USE FOR MINER WORKER ADDRESS)",
		},
		&cli.BoolFlag{
			Name:  "offline",
			Usage: "don't query chain state in interactive mode",
		},
		&cli.BoolFlag{
			Name:   "disable-auth",
			Usage:  "(insecure) disable api auth",
			Hidden: true,
		},
	},
	Description: "For setup instructions see 'lotus-wallet --help'",
	Action: func(cctx *cli.Context) error {
		log.Info("Starting lotus wallet")

		ctx := lcli.ReqContext(cctx)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		// Register all metric views
		if err := view.Register(
			metrics.DefaultViews...,
		); err != nil {
			log.Fatalf("Cannot register the view: %v", err)
		}

		lr, ks, err := openRepo(cctx)
		if err != nil {
			return err
		}
		defer lr.Close() // nolint

		lw, err := wallet.NewWallet(ks)
		if err != nil {
			return err
		}

		var w api.Wallet = lw
		if cctx.Bool("ledger") {
			ds, err := lr.Datastore(context.Background(), "/metadata")
			if err != nil {
				return err
			}

			w = wallet.MultiWallet{
				Local:  lw,
				Ledger: ledgerwallet.NewWallet(ds),
			}
		}

		address := cctx.String("listen")
		mux := mux.NewRouter()

		log.Info("Setting up API endpoint at " + address)

		if cctx.Bool("interactive") {
			var ag func() (v0api.FullNode, jsonrpc.ClientCloser, error)

			if !cctx.Bool("offline") {
				ag = func() (v0api.FullNode, jsonrpc.ClientCloser, error) {
					return lcli.GetFullNodeAPI(cctx)
				}
			}

			w = &InteractiveWallet{
				under:     w,
				apiGetter: ag,
			}
		} else {
			w = &LoggedWallet{under: w}
		}

		rpcApi := metrics.MetricedWalletAPI(w)
		if !cctx.Bool("disable-auth") {
			rpcApi = api.PermissionedWalletAPI(rpcApi)
		}

		rpcServer := jsonrpc.NewServer()
		rpcServer.Register("Filecoin", rpcApi)

		mux.Handle("/rpc/v0", rpcServer)
		mux.PathPrefix("/").Handler(http.DefaultServeMux) // pprof

		var handler http.Handler = mux

		if !cctx.Bool("disable-auth") {
			authKey, err := modules.APISecret(ks, lr)
			if err != nil {
				return xerrors.Errorf("setting up api secret: %w", err)
			}

			authVerify := func(ctx context.Context, token string) ([]auth.Permission, error) {
				var payload jwtPayload
				if _, err := jwt.Verify([]byte(token), (*jwt.HMACSHA)(authKey), &payload); err != nil {
					return nil, xerrors.Errorf("JWT Verification failed: %w", err)
				}

				return payload.Allow, nil
			}

			log.Info("API auth enabled, use 'lotus-wallet get-api-key' to get API key")
			handler = &auth.Handler{
				Verify: authVerify,
				Next:   mux.ServeHTTP,
			}
		}

		srv := &http.Server{
			Handler: handler,
			BaseContext: func(listener net.Listener) context.Context {
				ctx, _ := tag.New(context.Background(), tag.Upsert(metrics.APIInterface, "lotus-wallet"))
				return ctx
			},
		}


		go func() {
			<-ctx.Done()
			log.Warn("Shutting down...")
			if err := srv.Shutdown(context.TODO()); err != nil {
				log.Errorf("shutting down RPC server failed: %s", err)
			}
			log.Warn("Graceful shutdown successful")
		}()

		nl, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}

		return srv.Serve(nl)
	},
}

func openRepo(cctx *cli.Context) (repo.LockedRepo, types.KeyStore, error) {
	repoPath := cctx.String(FlagWalletRepo)
	r, err := repo.NewFS(repoPath)
	if err != nil {
		return nil, nil, err
	}

	ok, err := r.Exists()
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		if err := r.Init(repo.Worker); err != nil {
			return nil, nil, err
		}
	}

	lr, err := r.Lock(repo.Wallet)
	if err != nil {
		return nil, nil, err
	}

	ks, err := lr.KeyStore()
	if err != nil {
		return nil, nil, err
	}

	return lr, ks, nil
}
