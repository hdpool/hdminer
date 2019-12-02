package main

import (
	"io/ioutil"
	"strings"

	"github.com/hdpool/hdminer/liboo"

	"github.com/json-iterator/go"
)

func h5api_defhandler(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	//来自h5 websocket的消息请求消息分发
	switch reqmsg.Cmd {

	case MINER_CMD_HEARTBEAT:
		retmsg = reqmsg

	case MINER_CMD_GET_STATUS:
		retmsg = reqmsg
		retmsg.Para = oo.JsonData(g_miner_status)

	case MINER_CMD_GET_CONFIG:
		retmsg = reqmsg
		retmsg.Para = oo.JsonData(conf)

	case MINER_CMD_GET_CONFIG_FILE:
		buf, err := ioutil.ReadFile(blago_cfg)
		if nil != err {
			return oo.PackFatal("error."), nil
		}
		retmsg = reqmsg
		retmsg.Para = buf
	}
	return
}

func h5api_autoupdate(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	para := struct {
		AutoUpdate int64
	}{}
	if err = jsoniter.Unmarshal(reqmsg.Para, &para); err != nil {
		return oo.PackError(reqmsg.Cmd, oo.EPARAM, "json format"), nil
	}
	if para.AutoUpdate != 0 && para.AutoUpdate != 1 {
		return oo.PackError(reqmsg.Cmd, oo.EPARAM, ""), nil
	}
	retmsg = reqmsg

	return
}

func h5api_autostartup(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	para := struct {
		AutoStartup int64
	}{}
	if err = jsoniter.Unmarshal(reqmsg.Para, &para); err != nil {
		return oo.PackError(reqmsg.Cmd, oo.EPARAM, "json format"), nil
	}
	if para.AutoStartup != 0 && para.AutoStartup != 1 {
		return oo.PackError(reqmsg.Cmd, oo.EPARAM, ""), nil
	}
	retmsg = reqmsg

	return
}

func h5api_localnonce(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	para := MinerParaLocalNonce{}
	if g_db != nil {
		para.Nonces, err = g_db.GetLocalNonce(100)
		if err != nil {
			return oo.PackFatal("error."), nil
		}
	}

	retmsg = reqmsg
	retmsg.Para = oo.JsonData(para)

	return
}

func h5api_topnonce(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	para := MinerParaTopNonce{}
	if g_db != nil {
		para.Nonces, err = g_db.GetTopNonce(100)
		if err != nil {
			return oo.PackFatal("error."), nil
		}
	}

	retmsg = reqmsg
	retmsg.Para = oo.JsonData(para)

	return
}

func h5api_setconfig(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	para := conf
	if err := jsoniter.Unmarshal(reqmsg.Para, &para); err != nil {
		return oo.PackFatal("para error."), nil
	}
	//Todo : Check conf
	oo.LogD("get request %v", para)

	byteValue, err := ioutil.ReadFile(blago_cfg)
	if err != nil {
		return oo.PackFatal("read conf error."), nil
	}

	var result map[string]interface{}
	err = jsoniter.Unmarshal(byteValue, &result)
	if err != nil {
		return oo.PackFatal("conf unmarshal error."), nil
	}
	//只有如下三个是修改的
	// result["Paths"] = para.Paths
	result["AccountKey"] = para.AccountKey
	result["MinerName"] = para.MinerName

	jsidt, _ := jsoniter.MarshalIndent(result, "", "    ")
	// jsidt, _ := json.MarshalIndent(result, "\r", "\t")
	jsidt = []byte(strings.Replace(string(jsidt), "\n", "\r\n", -1) + "\r\n")
	err = ioutil.WriteFile(blago_cfg, jsidt, 0666)
	if err != nil {
		return oo.PackFatal("Failed to save miner conf."), nil
	}

	oo.LogD("to reload blago miner.conf")
	// reload conf
	loadConfig()

	retmsg = reqmsg
	retmsg.Para = oo.JsonData(conf)

	return
}

func h5api_getplist(ws *oo.WebSock, reqmsg *oo.RpcMsg) (retmsg *oo.RpcMsg, err error) {
	para := MinerParaGetPlistRsp{}
	// plist, _ := getPlist(3)
	// //合并，plist里有的，conf也有则是1. conf没则是0；plist里无而conf里有的，则是-1
	// para.Plist, _ = diffPlist(plist, conf.Paths)

	retmsg = reqmsg
	retmsg.Para = oo.JsonData(para)
	return
}
