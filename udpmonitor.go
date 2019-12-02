// +build windows
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/hdpool/hdminer/liboo"
)

func process_scan(buf []byte, n int) {
	bytes := uint64(0)
	for i := 2; i+7 < n; i += 8 { // 从第三个字节起才有用
		one_bytes := uint64(binary.LittleEndian.Uint64(buf[i : i+8]))
		bytes += one_bytes
	}

	if g_miner_status.ScanedBytes == 0 {
		g_miner_status.StartMsec = time.Now().UnixNano() / 1e6
	}
	g_miner_status.ScanedBytes += int64(bytes) * 4096
	g_miner_status.ScanPercent = g_miner_status.ScanedBytes * 10000 / g_miner_status.TotalBytes

	// 这里才需要修改秒数
	g_miner_status.ScanMsec = time.Now().UnixNano()/1e6 - g_miner_status.StartMsec

	// 算kb的话，则好与毫秒数相抵
	if g_miner_status.ScanMsec > 0 {
		g_miner_status.Speed = g_miner_status.ScanedBytes / g_miner_status.ScanMsec
	}
}

func process_log(buf []byte, n int) {
	nlen := bytes.IndexByte(buf[2:], 0)
	key := string(buf[2 : 2+nlen])

	nlen = bytes.IndexByte(buf[10:], 0)
	msg := string(buf[10 : 10+nlen])
	H5Log(key, msg)
}

func process_report(buf []byte, n int) {
	accid := uint64(binary.LittleEndian.Uint64(buf[2:10]))
	height := int64(binary.LittleEndian.Uint64(buf[10:18]))
	nonce := uint64(binary.LittleEndian.Uint64(buf[18:26]))
	dl := int64(binary.LittleEndian.Uint64(buf[26:34]))

	var m SubmitInfo
	m.AccountId = fmt.Sprintf("%d", accid)
	m.Nonce = fmt.Sprintf("%d", nonce)
	m.Height = height
	m.Deadline = dl
	m.Ts = time.Now().Unix()
	m.Coin = g_miner_status.MiningCoin

	submit_ch <- MinerSubmit{
		MinerInfo: MinerInfo{
			MinerName: conf.MinerName,
			Capacity:  conf.Capacity,
		},
		SubmitInfo: m,
	}
}

func MinerMoniter() {
	defer func() {
		if errs := recover(); errs != nil {
			oo.LogW("recover MinerMoniter. err=%v", errs)
		}
	}()

	udp_addr, err := net.ResolveUDPAddr("udp", ":60100")
	if err != nil {
		oo.LogD("Failed to addr %v", err)
		return
	}

	conn, err := net.ListenUDP("udp", udp_addr)
	if err != nil {
		oo.LogD("Failed to listen udp 60100 %v", err)
		return
	}
	defer conn.Close()
	if err = conn.SetReadBuffer(1024 * 1024 * 1); err != nil {
		oo.LogD("Failed to set read buff. err=%v", err)
	}

	var buf [1024]byte

	for {
		n, _, err := conn.ReadFromUDP(buf[0:])
		if err != nil {
			oo.LogD("Failed to ReadFromUDP %v ", err)
			break
		}
		switch buf[1] {
		case 0: // scan
			process_scan(buf[:n], n)
		case 1:
			process_report(buf[:n], n)
		case 2:
			process_log(buf[:n], n)
		}
	}
}
