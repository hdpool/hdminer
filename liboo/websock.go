package oo

// 1. server端从http升级，读取信息，可发出信息，及请求
// 2. client端自动连接，保持心跳，读取信息，可发出信息及请求。如何标记是主动关闭的或是死掉？

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/json-iterator/go"
)

type ReqCtxHandler = func(*ReqCtx, *RpcMsg) (*RpcMsg, error)

type WebSock struct {
	Ws   *websocket.Conn
	Ch   *Channel
	Data interface{} // 用于扩展

	// private
	sndid  uint64
	sndmap sync.Map // 从chk.sndid到回调ch的映射

	handleMap  sync.Map // 从cmd到处理函数的回调
	defHandler func(*WebSock, *RpcMsg) (*RpcMsg, error)

	// 定时处理器
	interval_fn func(*WebSock, int64)

	// 客户端专用
	ori_host string // 未解析的host
	url      *url.URL
	open_fn  func(*WebSock) // 打开时回调
	close_fn func(*WebSock) // 关闭时回调

	read_timeo int64 // 最后读超时

	co_sche bool // 是否协程方式处理新请求

	Sess          *Session // 一般是在open_fn里设置好这里，方便后面makereqctx
	ctxHandleMap  sync.Map
	defCtxHandler ReqCtxHandler

	// 统计事项
	create_ts   int64  // 创建时间
	read_ts     int64  // 最后读时间
	write_ts    int64  // 最后写时间
	read_count  uint64 // 读次数
	write_count uint64 // 写次数
	read_bytes  uint64 // 读流量
	write_bytes uint64 // 写流量
}

// 返回的err仅用于日志，若要回发消息，必须明确返回消息部分
type WsHandler = func(*WebSock, *RpcMsg) (*RpcMsg, error)

func InitWsClient(Scheme string, Host string, Path string, chmgr *ChannelManager) (*WebSock, error) {
	c := &WebSock{}

	c.ori_host = Host

	c.url = &url.URL{Scheme: Scheme, Host: Host, Path: Path}
	if pp := strings.IndexByte(c.url.Path, '?'); pp >= 0 {
		c.url.RawQuery = string(c.url.Path[pp+1:])
		c.url.Path = string(c.url.Path[:pp])
	}

	if chmgr == nil {
		chmgr = NewChannelManager(64)
	}
	c.Ch = chmgr.NewChannel()
	c.Ch.Data = c
	c.Sess = &Session{
		Ipv4:   0,
		Connid: c.Ch.GetSeq(),
	}
	c.create_ts = time.Now().Unix()

	go c.serializedSend()

	return c, nil
}

// 客户端进入自动重连，若不想堵塞可以用go程
func (c *WebSock) StartDial(chk_interval int64, def_ip string) {
	defer func() {
		if errs := recover(); errs != nil {
			LogW("recover StartDial %v. err=%v", c.url, errs)
		}
	}()

	checkfn := func(c *WebSock, def_ip string) {
		if c.Ws == nil {
			// 优先级：重新解析，则使用缓存，使用用默认
			if IP2Uint32(strings.Split(c.ori_host, ":")[0]) == 0 { // 是域名
				hosts := strings.Split(c.ori_host, ":")

				LogD("to nslookup %s", hosts[0])
				ns, err := net.LookupHost(hosts[0])
				if err != nil || len(ns) == 0 {
					LogW("nslookup %s err: %s", hosts[0], err.Error())
					if IP2Uint32(strings.Split(c.url.Host, ":")[0]) == 0 { // 无缓存，用默认
						ns = []string{def_ip}
					} //  else 用缓存
				}
				if len(ns) > 0 && ns[0] != "" { // 只取第一个
					c.url.Host = ns[0]
					if len(hosts) > 1 {
						c.url.Host = fmt.Sprintf("%s:%s", ns[0], hosts[1])
					}
					LogD("nslookup %s success, n=%d IP=%s", hosts[0], len(ns), ns[0])
				}
				// 若还是解析失败，就只能靠dialer
			}

			LogD("to dial %v, host: %s", c.url, c.ori_host)
			dialer := &websocket.Dialer{
				Proxy: http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
					ServerName:         c.ori_host,
				},
				HandshakeTimeout: time.Second * time.Duration(10), // 10秒放弃
			}
			wconn, _, err := dialer.Dial(c.url.String(), http.Header{"Host": []string{c.ori_host}})
			if err != nil {
				LogW("Failed to Dial %v, err=%v", c.url, err)
			} else {
				c.Ws = wconn
				LogD("succeed dial %v", c.url)

				go c.RecvRequest()
			}
		}

		if c.Ws != nil && c.interval_fn != nil {
			ms := time.Now().UnixNano() / 1e6
			c.interval_fn(c, ms)
		}
	}

	if chk_interval == 0 {
		chk_interval = 60 // 最次，60秒也得查一下。
	}

	for {
		// 先check一下
		checkfn(c, def_ip)

		select {
		case <-c.Ch.IsClosed(): // 主动关闭的
			return
		case <-time.After(time.Second * time.Duration(chk_interval)):
		}
	}
}

func (c *WebSock) SetIntervalHandler(fn func(c *WebSock, ms int64)) {
	c.interval_fn = fn
}
func (c *WebSock) SetOpenHandler(handler func(c *WebSock)) {
	c.open_fn = handler
}
func (c *WebSock) SetCloseHandler(handler func(c *WebSock)) {
	c.close_fn = handler
}

func (c *WebSock) ConnInfo() (s string) {
	if c != nil && c.Ws != nil {
		ip := c.Ws.RemoteAddr().String()
		if c.Sess != nil && c.Sess.Ipv4 != 0 {
			ip = Uint32ToIP(uint32(c.Sess.Ipv4))
		}
		s += fmt.Sprintf("c=[%d,%s]; r=(%s,%d,%d); w=(%s,%d,%d)<-->%s", c.Ch.GetSeq(),
			Ts2Fmt(c.create_ts), Ts2Fmt(c.read_ts), c.read_count, c.read_bytes,
			Ts2Fmt(c.write_ts), c.write_count, c.write_bytes, ip)
	}
	if c != nil && c.url != nil {
		s += fmt.Sprintf("(%v)", c.url)
	}
	return
}
func InitWebSock(w http.ResponseWriter, r *http.Request, chmgr *ChannelManager) (c *WebSock, err error) {
	c = &WebSock{}

	var upgrader = websocket.Upgrader{
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	c.Ws, err = upgrader.Upgrade(w, r, http.Header{
		"Access-Control-Allow-Origin":      []string{"*"},
		"Access-Control-Allow-Credentials": []string{"true"},
	})
	if err != nil {
		return nil, err
	}

	if chmgr == nil {
		chmgr = NewChannelManager(64)
	}
	c.Ch = chmgr.NewChannel()
	c.Ch.Data = c
	c.Sess = &Session{
		Ipv4:   uint(IP2Uint32(GetRealIP(r))),
		Connid: c.Ch.GetSeq(),
	}
	c.create_ts = time.Now().Unix()

	go c.serializedSend()

	return c, nil
}
func (c *WebSock) Close() {
	c.Ch.Close()

	if c.Ws != nil {
		c.Ws.Close()
		c.Ws = nil
	}
}

func (c *WebSock) OpenCoroutineFlag() {
	c.co_sche = true
}

func (c *WebSock) SetReadTimeout(read_timeo int64) {
	c.read_timeo = read_timeo
}
func (c *WebSock) IsReady() bool {
	return c.Ws != nil
}

func compatibleWsCtxHandler(ctx *ReqCtx, reqmsg *RpcMsg) (rspmsg *RpcMsg, err error) {
	c, _ := ctx.Ctx.(*WebSock)

	fn := c.defHandler
	if v, ok := c.handleMap.Load(ctx.Cmd); ok {
		fn, _ = v.(func(*WebSock, *RpcMsg) (*RpcMsg, error))
	}

	if fn != nil {
		rspmsg, err = fn(c, reqmsg)
	}
	return
}

// 老接口回调要二次查找
func (c *WebSock) HandleFunc(cmd string, fn interface{}) {
	if fn == nil {
		c.handleMap.Delete(cmd)
		c.ctxHandleMap.Delete(cmd)
		return
	}
	switch fn.(type) {
	case ReqCtxHandler:
		c.ctxHandleMap.Store(cmd, fn)
	case WsHandler:
		c.handleMap.Store(cmd, fn)
		c.ctxHandleMap.Store(cmd, compatibleWsCtxHandler)
	default:
		panic("handle func type error")
	}
}
func (c *WebSock) HandleFuncMap(mm map[string]ReqCtxHandler) {
	for cmd, nh := range mm {
		c.HandleFunc(cmd, nh)
	}
}
func (c *WebSock) DefHandleFunc(fn interface{}) {
	switch fn.(type) {
	case ReqCtxHandler:
		c.defCtxHandler = fn.(ReqCtxHandler)
	case WsHandler:
		c.defHandler = fn.(WsHandler)
		c.defCtxHandler = compatibleWsCtxHandler
	default:
		panic("handle func type error")
	}
}
func (c *WebSock) SkipHandleFunc(cmd string) {
	c.HandleFunc(cmd, func(*ReqCtx, *RpcMsg) (rsp *RpcMsg, err error) {
		return
	})
}

// 只有服务端需要主动调用
func (c *WebSock) RecvRequest() {
	defer func() {
		if errs := recover(); errs != nil {
			LogW("recover RecvRequest %s. err=%v", c.ConnInfo(), errs)
		}
		if c.close_fn != nil {
			c.close_fn(c)
		}
		// LogD("Exit RecvRequest. ws:%p, %s", c, c.ConnInfo())
		if c.Ws != nil {
			c.Ws.Close()
			c.Ws = nil
		}
	}()

	if c.open_fn != nil {
		c.open_fn(c)
	}

For:
	// 一边接收它发来的消息
	for {
		if c.read_timeo > 0 {
			dline := time.Now().Add(time.Second * time.Duration(c.read_timeo))
			c.Ws.SetReadDeadline(dline) // 超时本来就会断线
		}
		messageType, message, err := c.Ws.ReadMessage()
		if err != nil {
			// 忽略正常关闭、未正确关闭、未结的消息
			if !websocket.IsCloseError(err,
				websocket.CloseNoStatusReceived,
				websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure) {
				LogD("Close websocket %s. err=%v", c.ConnInfo(), err)
			}
			break For
		}
		// StatChg(PERFSTAT_INCOMING_TRAFFIC, uint64(len(message)))

		c.read_ts = time.Now().Unix() //测试代码TO BE delete
		c.read_count++
		c.read_bytes += uint64(len(message))

		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			// c.Ch.PushMsg(PackFatal("message type error"))
			LogW("logic error.") // 不可能返回其它，这里就不应该判断
			continue
		}
		if len(message) == 0 { // skip
			StatChg("NULL Message", 1) // 测试代码TO BE delete
			continue
		}
		// 解码message外层为json
		// StatChg(PERFSTAT_REQUEST, 1)

		rpcmsg := &RpcMsg{}
		if err = jsoniter.Unmarshal(message, rpcmsg); err != nil { //格式错了,不提示
			// c.Ch.PushMsg(PackFatal("json parse error"))
			LogD("%s json parse error. len=%d. msg=%s", c.ConnInfo(), len(message), Bytes2Str(message))
			continue
		}
		// StatChg(fmt.Sprintf("%s%s", PERFSTAT_REQUEST_PREFIX, rpcmsg.Cmd), 1)
		// LogD("Get cmd req: %s", rpcmsg.Cmd)

		// 先看看在不在等待回复里
		if rpcmsg.Chk != "" {
			if ch, ok := c.sndmap.Load(rpcmsg.Chk); ok {
				// LogD("get reply ch. %s, chk:%s", rpcmsg.Cmd, rpcmsg.Chk)
				ch.(chan *RpcMsg) <- rpcmsg
				continue
			}
		}

		// 再看有否注册了处理函数
		fn := c.defCtxHandler
		if v, ok := c.ctxHandleMap.Load(rpcmsg.Cmd); ok {
			// LogD("Get push hand fn, %s", rpcmsg.Cmd)
			fn, _ = v.(func(*ReqCtx, *RpcMsg) (*RpcMsg, error))
		}

		if fn == nil {
			LogD("skip %s", rpcmsg.Cmd)
			continue
		}

		rpcmsg.Sess = c.Sess
		ctx := MakeReqCtx(c, rpcmsg.Cmd, rpcmsg.Sess)

		if !c.co_sche {
			// 调用处理函数
			retmsg, err := fn(ctx, rpcmsg)
			if err != nil {
				LogD("Failed to process %s cmd: %s, para: %s, err: %v", c.ConnInfo(), rpcmsg.Cmd, string(rpcmsg.Para), err)
			}
			if retmsg != nil { // 若处理函数有返回，即是有回复了
				retmsg.Sess = nil
				c.Ch.PushMsg(retmsg)
			}
		} else {
			go func() {
				defer func() {
					if errs := recover(); errs != nil {
						LogW("recover ProcessReq %s. cmd=%s, err=%v", c.ConnInfo(), rpcmsg.Cmd, errs)
					}
				}()
				retmsg, err := fn(ctx, rpcmsg)
				if err != nil {
					LogD("Failed to process %s cmd: %s, para: %s, err: %v", c.ConnInfo(), rpcmsg.Cmd, string(rpcmsg.Para), err)
				}
				if retmsg != nil { // 若处理函数有返回，即是有回复了
					retmsg.Sess = nil
					c.Ch.PushMsg(retmsg)
				}
			}()
		}
	}
}

func (c *WebSock) serializedSend() {
	defer func() {
		if errs := recover(); errs != nil {
			LogW("recover serializedSend %s. err=%v", c.ConnInfo(), errs)
		}
	}()

For:
	for {
		select {
		case <-c.Ch.IsClosed():
			break For
		case m := <-c.Ch.RecvChan():
			// StatChg(PERFSTAT_OUTGOING_TRAFFIC, uint64(len(data)))
			if c.Ws != nil {
				rpcmsg, _ := m.(*RpcMsg)
				data, err := jsoniter.Marshal(rpcmsg)
				if err == nil && c.Ws != nil {
					err = c.Ws.WriteMessage(websocket.TextMessage, data)
					c.write_ts = time.Now().Unix() //测试代码TO BE delete
					c.write_count++
					c.write_bytes += uint64(len(data))
				}
				if err != nil {
					LogD("write %s to %s err: %v", rpcmsg.Cmd, c.ConnInfo(), err)
				}
			} else {
				c.Ch.PushMsg(m) //wait a min
				<-time.After(time.Second * time.Duration(1))
			}
		}
	}
}

func (c *WebSock) SendRpc(rpcmsg *RpcMsg) error {
	if c.Ws == nil {
		return errors.New("not ready")
	}

	c.Ch.PushMsg(rpcmsg)
	return nil
}
func (c *WebSock) SendRpcSafe(rpcmsg *RpcMsg) error {
	rpcmsg2 := *rpcmsg
	return c.SendRpc(&rpcmsg2)
}
func (c *WebSock) SendRpcWithResponse(rpcmsg *RpcMsg, sec int64) (rsp *RpcMsg, err error) {
	if c.Ws == nil {
		return nil, errors.New("not ready")
	}

	sndid := atomic.AddUint64(&c.sndid, 1)
	rpcmsg.Chk = fmt.Sprintf("%s.%d", rpcmsg.Chk, sndid)
	ch := make(chan *RpcMsg, 1)
	defer close(ch)

	c.sndmap.Store(rpcmsg.Chk, ch)
	defer c.sndmap.Delete(rpcmsg.Chk)

	c.Ch.PushMsg(rpcmsg)

	select {
	case rsp = <-ch:
		rsp.Chk = rpcmsg.Chk
		return rsp, nil
	case <-time.After(time.Second * time.Duration(sec)):
		return nil, errors.New("timeout")
	}
}

func (c *WebSock) SendRpcParseResponse(rpcmsg *RpcMsg, sec int64, pret interface{}) (err error) {
	rsp, err := c.SendRpcWithResponse(rpcmsg, sec)
	if err != nil {
		return
	}
	if rsp.Err != nil {
		err = errors.New(rsp.Err.Msg)
		return
	}
	if pret != nil {
		err = jsoniter.Unmarshal(rsp.Para, pret)
	}
	return
}

func ListenWsServer(addr string, webdir string, chmgr *ChannelManager,
	open_fn func(http.ResponseWriter, *http.Request, *WebSock) error) {

	var http_handler http.Handler
	if webdir != "" {
		http_handler = http.FileServer(http.Dir(webdir))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, exists := r.Header["Upgrade"]; !exists { //非websocket
			if http_handler != nil {
				http_handler.ServeHTTP(w, r)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}
		if open_fn == nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		client, err := InitWebSock(w, r, chmgr)
		if err != nil {
			LogD("Failed to init ws. err:%v", err)
			return
		}
		defer client.Close()

		// 不设置client.open_fn
		if err = open_fn(w, r, client); err != nil {
			LogD("on open websocket err: %v", err)
			data := fmt.Sprintf(`{"cmd":"%s", "err":{"ret":"%s", "msg":"%s"}}`,
				"fatal.error", EFATAL, err.Error())
			client.Ws.WriteMessage(websocket.TextMessage, []byte(data))
			return
		}

		// not return
		client.RecvRequest()
	})

	http.ListenAndServe(addr, mux)
}
