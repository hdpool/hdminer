package oo

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ReqCtx = struct {
	Ctx    interface{} // 所在环境，例如websock, ns, ...
	Cmd    string      // 原请求命令，例如logind.registe
	Sess   *Session    // 请求环境
	CoSche bool        // 是否在新起协程环境？方便判断堵塞情况
}

type Session = struct {
	Uid    uint64 `json:"uid,omitempty"`
	Key    string `json:"key,omitempty"`
	Gwid   string `json:"gwid,omitempty"`   // gw的svrmark
	Connid uint64 `json:"connid,omitempty"` // 连接ch
	Ipv4   uint   `json:"ipv4,omitempty"`
}

type Error = struct {
	Ret string `json:"ret,omitempty"` // 错误号
	Msg string `json:"msg,omitempty"` // 错误信息
	Eid string `json:"eid,omitempty"` // 出错时相关的ID,例如订单号
}

type RpcMsg = struct {
	Cmd  string          `json:"cmd,omitempty"`
	Mark string          `json:"mark,omitempty"` // 标志，通常用于细化转发
	Chk  string          `json:"chk,omitempty"`
	Sess *Session        `json:"sess,omitempty"`
	Err  *Error          `json:"err,omitempty"`
	Para json.RawMessage `json:"para,omitempty"`
}

type ParaNull = struct {
}

func MakeReqCtx(c interface{}, cmd string, sess *Session) (ctx *ReqCtx) {
	ctx = &ReqCtx{
		Ctx:  c,
		Cmd:  cmd,
		Sess: sess,
	}
	return
}

// 用于Redis存储的session
func GenSessid(uid uint64) string {
	return fmt.Sprintf("%d-%d", uid, time.Now().UnixNano())
}

// 用于下发给客户端
func GenKey(sessid, passwd string, uid, expire uint64) string {
	s := fmt.Sprintf("%s|%d|%d", sessid, uid, expire)
	// Todo: aes encrypt
	en := AesEncrypt([]byte(passwd), []byte(s))
	return string(Base58Encode([]byte(en)))
}

func GenImgCode() string {
	return string(randStr(6, 0))
}

func GenEmailCode() string {
	return string(randStr(6, 0))
}

func CheckPlainKey(key string) (sessid string, uid uint64, e error) {
	var expire uint64
	sss := strings.Split(string(key), "|")
	if len(sss) < 3 {
		return "", 0, NewError("key count error, key=%s", key)
	}
	sessid = sss[0]
	uid, _ = strconv.ParseUint(sss[1], 10, 64)
	expire, _ = strconv.ParseUint(sss[2], 10, 64)

	if expire < (uint64)(time.Now().Unix()) {
		return "", 0, NewError("key timeout, key=%s", key)
	}
	return sessid, uid, nil
}

func CheckKey(key string) (sessid string, uid uint64, e error) {
	s := Base58Decode([]byte(key))
	// Todo: aes decrypt
	var expire uint64
	sss := strings.Split(string(s), "|")
	if len(sss) < 3 {
		return "", 0, NewError("key count error, s=%s", s)
	}
	sessid = sss[0]
	uid, _ = strconv.ParseUint(sss[1], 10, 64)
	expire, _ = strconv.ParseUint(sss[2], 10, 64)

	if expire < (uint64)(time.Now().Unix()) {
		return "", 0, NewError("key timeout, s=%s", s)
	}
	return sessid, uid, nil
}

func CheckSession(sess *Session) (sessid string, uid uint64, e error) {
	if sess == nil {
		return "", 0, NewError("no sess")
	}
	return CheckPlainKey(sess.Key)
}

func PackErr(err string) *Error {
	return &Error{Ret: err, Msg: ErrStr(err)}
}

func PackError(cmd string, err string, msg string) *RpcMsg {
	if msg == "" {
		msg = ErrStr(err)
	}
	return &RpcMsg{Cmd: cmd, Err: &Error{Ret: err, Msg: msg}}
}

func PackSess(uid uint64, key string, gwid string, connid uint64) *Session {
	return &Session{Uid: uid, Key: key, Gwid: gwid, Connid: connid}
}

func PackFatal(msg string) *RpcMsg {
	return &RpcMsg{Cmd: "fatal.error", Err: &Error{Ret: EFATAL, Msg: msg}}
}

func PackRpcMsg(cmd string, para interface{}, sess *Session) *RpcMsg {
	return &RpcMsg{Cmd: cmd, Para: JsonData(para), Sess: sess}
}

func PackPara(cmd string, para interface{}) *RpcMsg {
	return &RpcMsg{Cmd: cmd, Para: JsonData(para)}
}

func PackNull(cmd string) *RpcMsg {
	return &RpcMsg{Cmd: cmd}
}

func PackRspMsg(reqmsg *RpcMsg, eret string, rsp interface{}, eid ...string) (rspmsg *RpcMsg) {
	rspmsg = reqmsg
	if eret == ESUCC {
		rspmsg.Para = JsonData(rsp)
	} else {
		rspmsg.Para = nil
		rspmsg.Err = PackErr(eret)
		if len(eid) > 0 {
			rspmsg.Err.Eid = eid[0]
		}
	}
	return
}

// 转化rpcerr, 清空err
func RpcReturn(reqmsg *RpcMsg, eret_rpcerr interface{}, rsp ...interface{}) (rspmsg *RpcMsg, err error) {
	rspmsg = reqmsg
	rspmsg.Para = nil

	var is_succ bool
	if eret, ok := eret_rpcerr.(string); ok && eret == ESUCC {
		is_succ = true
	}
	if eret_rpcerr == nil || is_succ {
		if len(rsp) > 0 {
			rspmsg.Para = JsonData(rsp[0])
		}
		return
	}
	switch eret_rpcerr.(type) {
	case string:
		eret, _ := eret_rpcerr.(string)
		rspmsg.Err = PackErr(eret)
	case *RpcError:
		rpcerr, _ := eret_rpcerr.(*RpcError)
		rspmsg.Err = PackErr(rpcerr.Errno())
	case error:
		rspmsg.Err = PackErr(ESERVER)
		err, _ = eret_rpcerr.(error)
	default:
		rspmsg.Err = PackErr(ESERVER)
	}

	return
}
