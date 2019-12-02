package main

import (
	"fmt"
	"time"

	"github.com/hdpool/hdminer/liboo"

	"github.com/json-iterator/go"
)

func checkScaning() bool {
	return g_submit_ts+conf.SubmitTimeout > time.Now().Unix()
}

func GetNewMiningInfo(newCoin string, minfo MiningInfo) {
	itr := ""
	if checkScaning() {
		itr = fmt.Sprintf("Intr [%s]", g_miner_status.MiningCoin)
	}

	H5Log("work", fmt.Sprintf("Start mining [%s]%d at %s. %s",
		newCoin, minfo.Height, oo.Ts2Fmt(time.Now().Unix()), itr))

	oo.LogD("Get new MiningInfo [%s]%v => [%s]%v",
		g_miner_status.MiningCoin, g_miner_status.MiningInfo, newCoin, minfo)

	g_miner_status.MiningInfo = minfo
	g_miner_status.Speed = 0
	g_miner_status.ScanedBytes = 0
	g_miner_status.ScanPercent = 0
	g_miner_status.BestNonce = ""
	g_miner_status.BestDL = 0
	g_miner_status.StartMsec = time.Now().UnixNano() / 1e6
	g_miner_status.ScanMsec = 0

	g_miner_status.MiningCoin = newCoin
	*g_miner_status.MiningInfos[newCoin] = minfo

	g_miner_status.WorkingStatus = -2
	if conf.MinerExe == MODE_EMBEDDED {
		g_miner_status.WorkingStatus = 2
		if g_miner != nil && conf.OpenScan {
			go func(oldCoin string) {
				g_miner.OnNewMiningInfo(minfo)
				if oldCoin == g_miner_status.MiningCoin && minfo.Height == g_miner_status.MiningInfo.Height { // 全局变量是新的
					H5Log("work", fmt.Sprintf("Stop mining [%s] %d at %s", oldCoin, minfo.Height, oo.Ts2Fmt(time.Now().Unix())))
				}
			}(newCoin)
		}
		conf.OpenScan = true
	}
}

// -1低于不动, 0相同不动, 1：打断
func CoinPriorityIntr(coinOld string, coinNew string) int {
	pOldPrio := len(conf.Coins)
	pNewPrio := len(conf.Coins)
	for i, c := range conf.Coins {
		if c == coinOld {
			pOldPrio = i
		}
		if c == coinNew {
			pNewPrio = i
		}
	}
	if pOldPrio < pNewPrio { // 越小越优先，当为新的大时，不中断
		return -1
	}
	if pOldPrio > pNewPrio {
		return 1
	}
	return 0
}

func poolapi_NewMiningInfo(msg *oo.RpcMsg) {
	var err error
	var minfo MiningInfo
	if err = jsoniter.Unmarshal(msg.Para, &minfo); err != nil {
		oo.LogD("Parse MiningInfo err=%v", err)
		return
	}
	newCoin := msg.Mark
	if newCoin == "" {
		newCoin = "BHD" // force null to BHD
	}

	if !oo.InArray(newCoin, conf.Coins) {
		oo.LogD("Skip Coin %s", newCoin)
		return
	}

	// 为代理时，无法得知进度，只能控制N秒内如果无提交，则非scaning
	// 被抢断的不会再扫同高度
	// 状态机： 未扫则启动，低优或同优在扫则考虑，高优在扫则不动。
	if !checkScaning() || CoinPriorityIntr(g_miner_status.MiningCoin, newCoin) >= 0 {
		// 当新来的高度更高、或同高而签名不同，才切
		m, _ := g_miner_status.MiningInfos[newCoin]
		if m.Height < minfo.Height || (m.Height == minfo.Height && m.GenerationSignature != minfo.GenerationSignature) {
			if checkScaning() { // 在扫，则考虑标志本块为未扫
				mold, _ := g_miner_status.MiningInfos[g_miner_status.MiningCoin]
				if conf.MinerExe == MODE_EMBEDDED {
					// 观察在扫的进度，低于7成则认为未扫此高度
					if g_miner_status.ScanPercent < conf.RescanRate {
						mold.Height--
						oo.LogD("%s bak height to %d", g_miner_status.MiningCoin, mold.Height)
					}
				} else {
					mold.Height--
					oo.LogD("%s bak height to %d", g_miner_status.MiningCoin, mold.Height)
				}
			}

			GetNewMiningInfo(newCoin, minfo)
		}
	}
}

func poolapi_Open(it *oo.WebSock) {
	g_miner_status.ConnStatus = 1

	// 为避免界面卡顿，延后一点点时间再发，则需要传参
	go func(it *oo.WebSock) {
		<-time.After(3 * time.Second)
		// 只发送就好，不用等回复，已经有注册处理。
		msg := &oo.RpcMsg{
			Cmd:  POOLMGR_CMD_GETMININGINFO,
			Para: []byte("{}"),
		}
		for _, coin := range conf.Coins {
			msg.Mark = coin
			it.SendRpcSafe(msg)
		}
		oo.StatChg("send_mininginfo", 1)
	}(it)
}

func poolapi_Close(it *oo.WebSock) {
	g_miner_status.ConnStatus = -1
}

func poolapi_Interval(it *oo.WebSock, ms int64) {
	is_timeo := false
	last_snt_ms, ok := it.Data.(int64)
	if !ok || last_snt_ms+5000 < ms {
		it.Data = ms
		is_timeo = true
	}

	if is_timeo {
		actor.SendHeartbeat(it, ms)
		oo.StatChg("send_heartbeat", 1)

		msg := &oo.RpcMsg{
			Cmd:  POOLMGR_CMD_GETMININGINFO,
			Para: []byte("{}"),
		}
		for _, coin := range conf.Coins {
			msg.Mark = coin
			it.SendRpcSafe(msg)
		}
		oo.StatChg("send_mininginfo", 1)
	}
}

func poolapi_PushMinningInfo(it *oo.WebSock, msg *oo.RpcMsg) (rsp *oo.RpcMsg, err error) {
	oo.StatChg("recv_mininginfo", 1)
	// 处理推送的更新块
	poolapi_NewMiningInfo(msg)

	return
}

func poolapi_Heartbeat(it *oo.WebSock, msg *oo.RpcMsg) (rsp *oo.RpcMsg, err error) {
	oo.StatChg("recv_heartbeat", 1)
	return
}
