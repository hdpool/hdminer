package main

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/hdpool/hdminer/liboo"
)

type Actor interface {
	// //
	Close()
	// 发送心跳到矿池
	SendHeartbeat(it *oo.WebSock, ms int64)
	// miner请求挖矿信息
	ProcGetMiningInfo(r *http.Request) (err error)
	// miner提交nonce
	ProcSubmitNonce(r *http.Request, sub SubmitInfo) (err error)
	// 是否检查DL正确性
	IsCheckDL() bool
	// 检查plotter_id
	CheckPlotterId(pid uint64) bool
}

//
// ==== impl hdminer ====
//
type ActorMiner struct {
}

func NewActorMiner() Actor {
	g_miner = InitMiner(conf.RealPaths, conf.CacheSize, conf.CacheSize2, conf.UseHDDWakeUp, conf.Inset)
	return &ActorMiner{}
}

func (this *ActorMiner) Close() {
	g_miner.Close()
}

func (this *ActorMiner) SendHeartbeat(it *oo.WebSock, ms int64) {
	para := PoolmgrParaHeartbeatReq{
		MinerInfo: MinerInfo{
			AccountKey: conf.AccountKey,
			MinerName:  conf.MinerName,
			MinerMark:  GMinerMark,
			Capacity:   conf.Capacity,
		},
	}
	msg := &oo.RpcMsg{Cmd: POOLMGR_CMD_HEARTBEAT, Para: oo.JsonData(para)}
	for _, coin := range conf.Coins {
		msg.Mark = coin
		it.SendRpcSafe(msg)
	}
}

func (this *ActorMiner) ProcGetMiningInfo(r *http.Request) (err error) {
	return
}

func (this *ActorMiner) ProcSubmitNonce(r *http.Request, sub SubmitInfo) (err error) {
	submit_ch <- MinerSubmit{
		MinerInfo: MinerInfo{
			MinerName: conf.MinerName,
			Capacity:  conf.Capacity,
		},
		SubmitInfo: sub,
	}

	return
}

func (this *ActorMiner) IsCheckDL() bool { return false }

func (this *ActorMiner) CheckPlotterId(pid uint64) bool {
	if _, ok := GAllPlotPaths.AllAccIds[pid]; !ok {
		return false
	}
	return true
}

//
// ==== impl hdproxy ====
//
type ActorProxy struct {
	ActorMiner
}

func NewActorProxy() Actor {
	return &ActorProxy{}
}

func (this *ActorProxy) Close() {}

func (this *ActorProxy) IsCheckDL() bool { return true }

//
// ==== impl center_proxy ====
//
type ActorCenterProxy struct {
	miners sync.Map
}

type MinerHB struct {
	Capacity int64
	LastTime int64
}

func NewActorCenterProxy() Actor {
	return &ActorCenterProxy{}
}

func (this *ActorCenterProxy) Close() {}

func (this *ActorCenterProxy) SendHeartbeat(it *oo.WebSock, ms int64) {
	this.miners.Range(func(k, v interface{}) bool {
		val := v.(MinerHB)
		if ms/1000-val.LastTime > 10 {
			this.miners.Delete(k)
			return true
		}
		para := PoolmgrParaHeartbeatReq{
			MinerInfo: MinerInfo{
				AccountKey: conf.AccountKey,
				MinerName:  k.(string),
				MinerMark:  GMinerMark,
				Capacity:   val.Capacity,
			},
		}
		msg := &oo.RpcMsg{Cmd: POOLMGR_CMD_HEARTBEAT, Para: oo.JsonData(para)}
		for _, coin := range conf.Coins {
			msg.Mark = coin
			it.SendRpcSafe(msg)
		}
		return true
	})
}

func (this *ActorCenterProxy) getMinerNameCapacity(r *http.Request) (miner_name string, capacity int64, err error) {
	str1 := r.Header.Get("x-minername")
	str2 := r.Header.Get("x-capacity")
	if len(str1) > 0 && len(str2) > 0 {
		var num int64
		num, err = strconv.ParseInt(str2, 10, 64)
		if nil == err {
			miner_name, capacity = str1, num
			return
		}
	}

	err = oo.NewError("unknown x-minername[%s] x-capacity[%s] %v", str1, str2, err)

	return
}

func (this *ActorCenterProxy) ProcGetMiningInfo(r *http.Request) (err error) {
	miner_name, capacity, err := this.getMinerNameCapacity(r)
	if nil != err {
		return
	}
	this.miners.Store(miner_name, MinerHB{capacity, time.Now().Unix()})

	return
}
func (this *ActorCenterProxy) ProcSubmitNonce(r *http.Request, sub SubmitInfo) (err error) {
	miner_name, capacity, err := this.getMinerNameCapacity(r)
	if nil != err {
		return
	}
	submit_ch <- MinerSubmit{
		MinerInfo: MinerInfo{
			MinerName: miner_name,
			Capacity:  capacity,
		},
		SubmitInfo: sub,
	}

	return
}

func (this *ActorCenterProxy) IsCheckDL() bool { return true }

func (this *ActorCenterProxy) CheckPlotterId(pid uint64) bool { return true }
