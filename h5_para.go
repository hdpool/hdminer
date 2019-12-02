package main

// ------------------------------------------------
const MINER_CMD_HEARTBEAT = "miner.heartbeat"

// ------------------------------------------------
const MINER_CMD_GET_CONFIG = "miner.get_config"

// ------------------------------------------------
const MINER_CMD_GET_CONFIG_FILE = "miner.get_config_file"

type MinerParaConfig ClientConfig

// ------------------------------------------------
const MINER_CMD_SET_CONFIG = "miner.set_config"

// ------------------------------------------------
const MINER_CMD_GET_PLIST = "miner.get_plist"

type Plist struct {
	Path     string `json:"path"`
	Capacity int64  `json:"capacity"`
	Flag     int64  `json:"flag"`
}

type MinerParaGetPlistRsp struct {
	Plist []Plist `json:"plist"`
}

// ------------------------------------------------
const MINER_CMD_GET_STATUS = "miner.get_status"

type Progress struct {
	Speed       int64  `json:"speed"`        //KB/s, 要除以1024用MB来显示
	TotalBytes  int64  `json:"total_bytes"`  //
	ScanedBytes int64  `json:"scaned_bytes"` //
	ScanPercent int64  `json:"scan_percent"` //10000分比
	InSet       string `json:"inset"`        //指令集
	BestNonce   string `json:"best_nonce"`   //当前最好dealline时的nonce
	BestDL      int64  `json:"best_dl"`      //当前最好的dealline
	StartMsec   int64  `json:"start_msec"`   //起始时间
	ScanMsec    int64  `json:"scan_msec"`    //已扫描多少毫秒
}

type MinerParaStatus struct {
	MinerInfo
	MiningInfo
	MiningCoin    string                 `json:"mining_coin"` //正在/最后挖的币
	WorkingStatus int64                  `json:"work_status"` //-2: Not moniter, -1: Error, 0: Doing, 1: Good, 2: embedded
	ConnStatus    int64                  `json:"conn_status"`
	WarningStr    string                 `json:"warning_str"` //
	Progress                             //进度部分。只在embedded情况下有用
	MiningInfos   map[string]*MiningInfo `json:"mining_infos"` //各币种的MiningInfo情况
}

// ------------------------------------------------
const MINER_CMD_LOCAL_NONCE = "miner.get_local_nonce"

type MinerParaLocalNonce struct {
	Nonces []SubmitInfo `json:"nonces"`
}

// ------------------------------------------------
const MINER_CMD_TOP_NONCE = "miner.get_top_nonce"

type MinerParaTopNonce MinerParaLocalNonce

// ------------------------------------------------
const MINER_CMD_SET_AUTOUPDATE = "miner.auto_update"

// ------------------------------------------------
const MINER_CMD_SET_AUTOSTARTUP = "miner.auto_startup"

// ------------------------------------------------
const MINER_CMD_GET_LOG = "miner.get_log"

type MinerParaLog struct {
	lines []string //一行一回车
}
