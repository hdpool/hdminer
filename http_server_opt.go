package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/hdpool/hdminer/liboo"
)

func H5Log(key, msg string) {
	oo.LogD("GOLOG %s: %s", key, msg)

	para := []string{fmt.Sprintf("%s: %s", key, msg)}

	g_chanmgr.PushAllChannelMsg(&oo.RpcMsg{
		Cmd:  MINER_CMD_GET_LOG,
		Para: oo.JsonData(para),
	}, nil)
}

type MinerSubmit struct {
	MinerInfo
	SubmitInfo
}

var (
	submit_ch = make(chan MinerSubmit, 1024)
)

func ProcMinerSubmit() {
	for {
		doProcMinerSubmit()
		time.Sleep(time.Second * 5)
	}
}

func doProcMinerSubmit() {
	defer func() {
		if err := recover(); nil != err {
			oo.LogW("panic : %v", err)
		}
	}()

	var submit sync.Map

	type Key struct {
		coin  string
		miner string
	}

	type Val struct {
		height int64
		best   int64
	}

	getf := func(coin, miner string) (height, best int64, ok bool) {
		v, ok := submit.Load(Key{coin, miner})
		if ok {
			vv := v.(Val)
			height, best = vv.height, vv.best
		}
		return
	}

	setf := func(coin, miner string, height, best int64) {
		submit.Store(Key{coin, miner}, Val{height, best})
	}

	gms := &g_miner_status
	for sub := range submit_ch {
		if sub.Coin != gms.MiningCoin || sub.Height != gms.Height {
			oo.LogD("sub.Coin[%v] != gms.MiningCoin[%v] || sub.Height[%v] != gms.Height[%v]",
				sub.Coin, gms.MiningCoin, sub.Height, gms.Height)
			continue
		}
		g_submit_ts = time.Now().Unix()

		// 检查plotter_id
		accid, _ := strconv.ParseUint(sub.AccountId, 10, 64)
		if !actor.CheckPlotterId(accid) {
			H5Log("error", fmt.Sprintf("AccountId=%s Not Allow, config error???.",
				sub.AccountId))
			continue
		}

		// 校验dl是否正确
		use_dl := sub.Deadline / gms.BaseTarget
		if actor.IsCheckDL() {
			// nonce, _ := strconv.ParseUint(sub.Nonce, 10, 64)
			// testdl := CalcDeadline(uint64(gms.Height), gms.GenerationSignature, accid, nonce)
			// if testdl != uint64(sub.Deadline) {
			// 	H5Log("SKIP", fmt.Sprintf("height=%d nonce=[%s]%s DL=%d, DL?=%d",
			// 		sub.Height, sub.AccountId, sub.Nonce, use_dl, testdl/uint64(gms.BaseTarget)))
			// 	continue
			// }
		}

		// . 先比较单个miner
		height, best, ok := getf(gms.MiningCoin, sub.MinerName)
		if ok && height == gms.Height && sub.Deadline >= best {
			H5Log("SKIP", fmt.Sprintf("height=%d nonce=[%s]%s DL=%d",
				sub.Height, sub.AccountId, sub.Nonce, use_dl))
			continue
		}
		setf(sub.Coin, sub.MinerName, sub.Height, sub.Deadline)

		// . 再比较最好的dl
		if 0 == gms.BestDL || use_dl < gms.BestDL {
			gms.BestDL, gms.BestNonce = use_dl, sub.Nonce
			oo.LogD("onNewNonce %d %s %d", sub.Height, sub.Nonce, use_dl)
		}

		// 提交到服务端
		para := PoolmgrParaSubmitNonceReq{
			MinerInfo: MinerInfo{
				AccountKey: conf.AccountKey,
				MinerName:  sub.MinerName,
				Capacity:   sub.Capacity,
			},
			Submit: []SubmitInfo{sub.SubmitInfo},
		}
		msg := &oo.RpcMsg{
			Cmd:  POOLMGR_CMD_SUBMITNONCE,
			Para: oo.JsonData(para),
			Mark: gms.MiningCoin,
		}
		if ws_pool == nil || ws_pool.SendRpc(msg) != nil {
			g_cache_nonce = append(g_cache_nonce, sub.SubmitInfo)
		}

		// 修正用于显示、存储
		sub.Deadline = use_dl
		H5Log("Nonce", fmt.Sprintf("height=%d nonce=[%s]%s DL=%d",
			sub.Height, sub.AccountId, sub.Nonce, sub.Deadline))

		// 发现一个则给client一个本地nonce
		paraNonce := MinerParaLocalNonce{
			Nonces: []SubmitInfo{sub.SubmitInfo},
		}
		g_chanmgr.PushAllChannelMsg(&oo.RpcMsg{
			Cmd:  MINER_CMD_LOCAL_NONCE,
			Para: oo.JsonData(paraNonce),
		}, nil)

		// db opt
		if g_db != nil {
			err := g_db.AddNonce(sub.SubmitInfo)
			if err != nil {
				oo.LogD("Failed to add nonce. err=%v", err)
				continue
			}
			// 是不是最好的100之一
			n, err := g_db.GetLessCount(sub.Deadline)
			if err != nil {
				oo.LogD("Failed to get less count. err=%v", err)
				continue
			}
			if n < 100 /*&& g_chanmgr != nil*/ {
				g_chanmgr.PushAllChannelMsg(&oo.RpcMsg{
					Cmd:  MINER_CMD_TOP_NONCE,
					Para: oo.JsonData(paraNonce),
				}, nil)
			}
		}
	}
}
