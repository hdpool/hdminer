package main

import (
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/hdpool/hdminer/liboo"
)

var (
	g_chanmgr      *oo.ChannelManager
	g_cache_nonce  []SubmitInfo
	g_miner_status MinerParaStatus
	g_submit_ts    int64

	http_handler http.Handler
)

type H5ServerIntr struct {
	last_ms int64
	isrun   bool
	mux     *http.ServeMux
	addr    string
}

func (it *H5ServerIntr) h5TickStatus() {
	defer func() {
		if errs := recover(); errs != nil {
			oo.LogW("recover h5TickStatus. err=%v", errs)
		}
	}()
	last_miner_status := MinerParaStatus{}

	var svr_closed_ch = make(chan struct{})
	close(svr_closed_ch) // 先关，以便触发创建

	for {
		select {
		case <-time.After(time.Millisecond * 30): // 30微秒一次
			// 实时播报状态
			if !reflect.DeepEqual(g_miner_status, last_miner_status) {
				last_miner_status = g_miner_status
				g_chanmgr.PushAllChannelMsg(&oo.RpcMsg{
					Cmd:  MINER_CMD_GET_STATUS,
					Para: oo.JsonData(g_miner_status),
				}, nil)
			}

			if len(g_cache_nonce) == 0 || ws_pool == nil || !ws_pool.IsReady() {
				continue
			}
			para := PoolmgrParaSubmitNonceReq{
				MinerInfo: MinerInfo{
					AccountKey: conf.AccountKey,
					MinerName:  conf.MinerName,
					Capacity:   conf.Capacity,
				},
				Submit: g_cache_nonce,
			}
			msg := &oo.RpcMsg{
				Cmd:  POOLMGR_CMD_SUBMITNONCE,
				Para: oo.JsonData(para),
			}
			ws_pool.SendRpc(msg)

			g_cache_nonce = []SubmitInfo{}

		case <-svr_closed_ch:
			oo.LogD("Re create hdserver.")
			svr_closed_ch = make(chan struct{})
			go func(it *H5ServerIntr, ch chan struct{}) {
				// 完全新建的mux
				http.ListenAndServe(it.addr, it.mux)
				time.Sleep(1 * time.Second) // 要缓一下，否则可能死循环
				close(ch)
			}(it, svr_closed_ch)
		}
	}
}

func InitH5Server() *H5ServerIntr {
	it := H5ServerIntr{}

	it.mux = http.NewServeMux()
	it.mux.HandleFunc("/burst", minerHandler)
	it.mux.HandleFunc("/", h5Handler)

	it.addr = fmt.Sprintf(":%d", conf.Port)

	web_dir := GWorkDir + "webui"
	http_handler = http.FileServer(http.Dir(web_dir))

	g_chanmgr = oo.NewChannelManager(64)

	go it.h5TickStatus()

	return &it
}

func h5Handler(w http.ResponseWriter, r *http.Request) {
	if _, exists := r.Header["Upgrade"]; !exists {
		// 非websocket
		http_handler.ServeHTTP(w, r)
		return
	}

	client, err := oo.InitWebSock(w, r, g_chanmgr)
	if err != nil {
		oo.LogD("Failed to create ws.")
		return
	}
	defer client.Close()

	client.DefHandleFunc(h5api_defhandler)
	client.HandleFunc(MINER_CMD_SET_AUTOUPDATE, h5api_autoupdate)
	client.HandleFunc(MINER_CMD_SET_AUTOSTARTUP, h5api_autostartup)
	client.HandleFunc(MINER_CMD_LOCAL_NONCE, h5api_localnonce)
	client.HandleFunc(MINER_CMD_TOP_NONCE, h5api_topnonce)
	client.HandleFunc(MINER_CMD_SET_CONFIG, h5api_setconfig)
	client.HandleFunc(MINER_CMD_GET_PLIST, h5api_getplist)

	client.RecvRequest()
}

func minerHandler(w http.ResponseWriter, r *http.Request) {
	// 三种业务：1. 取信息，直接返回；2. 推nonce，集中上传；3. 其它透传等回复
	rtype := r.FormValue("requestType")
	oo.StatChg(fmt.Sprintf("miner_%s", rtype), 1)

	if rtype == "getMiningInfo" {
		err := actor.ProcGetMiningInfo(r)
		if nil != err {
			oo.LogD("actor.ProcGetMiningInfo err %v", err)
			w.WriteHeader(http.StatusForbidden)
		}
		// {"height":104818,"generationSignature":"a33b784bc007586956e531ffd37c4d32d94df407df2df6467568687c4b23128e","baseTarget":"103524","targetDeadline":31536000,"requestProcessingTime":0}
		buf := oo.JsonData(g_miner_status.MiningInfo)
		w.Write(buf)
		return
	}

	if rtype == "submitNonce" {
		// POST /burst?requestType=submitNonce&accountId=%llu&nonce=%llu&deadline=%llu
		dl, _ := strconv.ParseInt(r.FormValue("deadline"), 10, 64)
		sn := SubmitInfo{
			AccountId: r.FormValue("accountId"),
			Nonce:     r.FormValue("nonce"),
			Deadline:  dl,
			Ts:        time.Now().Unix(),
			Coin:      g_miner_status.MiningCoin,
		}

		if g_miner_status.MiningInfo.BaseTarget > 0 {
			dl /= g_miner_status.MiningInfo.BaseTarget
		}
		// {"result":"success","deadline":%d,"targetDeadline":86400,"height":%d,"requestProcessingTime":0}
		retmsg := struct {
			Result                string `json:"result"`
			Deadline              int64  `json:"deadline"`
			TargetDeadline        int64  `json:"targetDeadline"`
			Height                int64  `json:"height"`
			RequestProcessingTime int64  `json:"requestProcessingTime"`
		}{
			Result:                "success",
			Deadline:              dl,
			TargetDeadline:        g_miner_status.MiningInfo.TargetDeadline,
			Height:                g_miner_status.MiningInfo.Height,
			RequestProcessingTime: g_miner_status.MiningInfo.RequestProcessingTime,
		}
		w.Write(oo.JsonData(retmsg))

		sn.Height = g_miner_status.MiningInfo.Height

		err := actor.ProcSubmitNonce(r, sn)
		if nil != err {
			oo.LogD("actor.ProcSubmitNonce err %v", err)
			w.WriteHeader(http.StatusForbidden)
		}

		return
	}

	if ws_pool == nil {
		oo.LogD("websocket Not init ")
		w.Write([]byte(""))

		return
	}

	// 不再透传其它
	w.Write([]byte(""))
}
