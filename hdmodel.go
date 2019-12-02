package main

//
// ==========================================================
// miner <--> poolmgr
// ==========================================================
//

type MinerInfo struct {
	AccountKey string `json:"account_key"`
	MinerName  string `json:"miner_name"`
	MinerMark  string `json:"miner_mark"`
	Capacity   int64  `json:"capacity"`
}

type MiningInfo struct {
	Height                int64  `json:"height"`
	GenerationSignature   string `json:"generationSignature"`
	BaseTarget            int64  `json:"baseTarget"`
	TargetDeadline        int64  `json:"targetDeadline"`
	RequestProcessingTime int64  `json:"requestProcessingTime"`
}

type SubmitInfo struct {
	AccountId string `json:"accountId" db:"AccountId"`
	Coin      string `json:"coin" db:"Coin"`
	Nonce     string `json:"nonce" db:"Nonce"`
	Deadline  int64  `json:"deadline" db:"Deadline"`
	Height    int64  `json:"height" db:"Height"` // 请求时不提交
	Ts        int64  `json:"ts" db:"Ts"`
}

//
// 心跳
//
const POOLMGR_CMD_HEARTBEAT = "poolmgr.heartbeat"

type PoolmgrParaHeartbeatReq struct {
	MinerInfo
}

//
// submit nonce 不返回任何内容
//
const POOLMGR_CMD_SUBMITNONCE = "poolmgr.submit_nonce"

type PoolmgrParaSubmitNonceReq struct {
	MinerInfo
	Submit []SubmitInfo `json:"submit"`
}

//
// 透传请求
//
const POOLMGR_CMD_MINER_REQUEST = "poolmgr.miner_request"

type PoolmgrParaCommonReq struct {
	MinerInfo
	Uri string `json:"uri"` // heartbeat和submit_nonce时，为空
}

type PoolmgrParaCommonRsp []byte

//
// 请求 mining info
//
const POOLMGR_CMD_GETMININGINFO = "mining_info" // miner使用
const POOLMGR_CMD_GET_MINING_INFO = "poolmgr.get_mining_info"

//
// 推送 mining info
//
const POOLMGR_CMD_PUSHMININGINFO = "poolmgr.mining_info"

type PoolmgrParaMiningInfo MiningInfo
