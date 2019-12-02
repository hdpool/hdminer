package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hdpool/hdminer/liboo"
)

var (
	GWorkDir    string
	GServerName string
	GVersion    string = "20190910"
	GMinerMark  string
	ws_pool     *oo.WebSock //
	g_db        *Sqlite     //
	g_miner     *Miner      //
	actor       Actor       //
)

func init() {
	hostname, _ := os.Hostname()
	GMinerMark = fmt.Sprintf("%s.%s.%s", hostname, filepath.Base(os.Args[0]), GVersion)
}

func ExitAll() {
	oo.LogD("Exiting...")
	QuitGui()
	os.Exit(0)
}

func main() {
	defer func() {
		errs := recover()
		if errs == nil {
			oo.LogD("normal exit.")
			return
		}
		oo.LogW("recover: %v", errs)
		// WinTips(GServerName, fmt.Sprintf("Unexpected exit: %v", errs))
	}()
	fmt.Printf("hdminer start.\n")

	//. 取得工作目录
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	GWorkDir = dir + string(os.PathSeparator)
	GServerName = strings.Split(filepath.Base(os.Args[0]), ".")[0]

	// runtime.GOMAXPROCS(runtime.NumCPU())
	//安排日志
	if len(os.Args) == 1 || os.Args[1] != "-d" {
		oo.InitLog(GWorkDir, GServerName, "", nil)

		if f, err := os.OpenFile(GWorkDir+"warn.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err == nil {
			redirectStderr(f)
		}
	} else {
		go http.ListenAndServe(":60001", nil)
	}

	//. 读取配置
	err := loadConfig()
	if err != nil {
		oo.LogW("Failed to read config.err=%v", err)
		WinTips(GServerName, "Configuration file error: miner.conf")
		ExitAll()
	}
	defer actor.Close()

	//. 判断是否已启动，若已启动，则唤醒界面，退出
	local_url := fmt.Sprintf("http://127.0.0.1:%d", conf.Port)
	resp, err := http.Get(local_url)
	if err == nil {
		resp.Body.Close()
		if conf.OpenWeb { //仅在时提示
			WinTips(GServerName, "The previous program is running, please make sure it has exited")
		}
		os.Exit(0) // ExitAll()
	}
	oo.LogD("Config: %#v, Loaded Paths: %#v", conf, GAllPlotPaths.AllPaths)

	g_miner_status = MinerParaStatus{
		MinerInfo: MinerInfo{
			AccountKey: conf.AccountKey,
			MinerName:  conf.MinerName,
			Capacity:   conf.Capacity,
		},
		ConnStatus: 0,
		Progress: Progress{
			TotalBytes: conf.CapBytes,
			InSet:      conf.Inset,
		},
		MiningInfos: map[string]*MiningInfo{},
		MiningCoin:  conf.Coins[0], //初始
	}
	for _, c := range conf.Coins {
		g_miner_status.MiningInfos[c] = new(MiningInfo)
	}

	//. 启动上连websocket
	ws_pool, _ = oo.InitWsClient("wss", conf.LinkPoint, "/", nil)
	defer ws_pool.Close()

	ws_pool.SetOpenHandler(poolapi_Open)
	ws_pool.SetCloseHandler(poolapi_Close)
	ws_pool.SetIntervalHandler(poolapi_Interval)
	ws_pool.HandleFunc(POOLMGR_CMD_PUSHMININGINFO, poolapi_PushMinningInfo)
	ws_pool.HandleFunc(POOLMGR_CMD_GETMININGINFO, poolapi_PushMinningInfo)
	ws_pool.HandleFunc(POOLMGR_CMD_HEARTBEAT, poolapi_Heartbeat)
	ws_pool.SetReadTimeout(15)                //15秒收不到任何消息即重连
	go ws_pool.StartDial(1, "27.102.127.123") //每秒检查连接

	//启动数据库
	g_db, err = InitSqlite()
	if err != nil {
		oo.LogW("Failed to create db. err=%v", err)
		ExitAll()
	}

	// 等待处理上报nonce
	go ProcMinerSubmit()

	InitH5Server()
	if conf.OpenWeb {
		openUrlWithDefaultBrowser(local_url)
	}

	//. 拉起托盘图标
	_ = InitGui()

	//. 等待退出
	for {
		oo.StatChg("CPUDOS_CHECK", 1)
		time.Sleep(5 * time.Second)
	}
}
