// +build darwin linux !windows

package main

import (
	"os"
	"syscall"
)

/*
#cgo LDFLAGS: -L. -lminer_darwin -lpthread
#include "miner/scheduler.h"
*/
import "C"

func CalcDeadline(height uint64, sig string, accid uint64, nonce uint64) uint64 {
	x := C.calc_dl(C.ulonglong(height), C.CString(sig), C.ulonglong(accid), C.ulonglong(nonce))
	return uint64(x)
}

func WinTips(title string, msg string) {

}

func DelayScan() {
}

type Miner struct {
}

func InitMiner(Paths []string, cache1 int64, cache2 int64, wakeup bool, inset string) *Miner {
	return &Miner{}
}

func (m *Miner) Close() {

}

func (m *Miner) GetInset() string {
	return ""
}

func (m *Miner) DelayScan() {

}

func (m *Miner) OnNewMiningInfo(minfo MiningInfo) {
}

func redirectStderr(f *os.File) (err error) {
	return syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
