// +build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/hdpool/hdminer/liboo"
)

/*
#cgo LDFLAGS: -L. -lminer_windows -lws2_32
#include "miner/scheduler.h"
*/
import "C"

// var GCpumark string
// var GInSet string

type Miner struct {
	paths []string

	// closech  chan struct{}
	closedch chan struct{}
	wait_chs chan int

	is_not_first bool
}

func ScheInit(Paths []string, cache1 int64, cache2 int64, wakeup1 int, inset string) {
	C.sche_init(C.int(len(Paths)), C.unsigned(cache1), C.unsigned(cache2), C.int(wakeup1), C.CString(inset))
}

func ScheCleanup() {
	C.sche_cleanup()
}

func ScheRunNew(i int, dir string, height uint64, sig string, tdl uint64, btg uint64) {
	C.sche_runnew(C.int(i), C.CString(dir), C.ulonglong(height),
		C.CString(sig),
		C.ulonglong(tdl), C.ulonglong(btg))
}
func ScheSetRunFlag(flag int) {
	C.sche_set_runflag(C.int(flag))
}

func ScheGetInset() string {
	return fmt.Sprintf("%s", C.GoString(C.sche_get_inset()))
}

func CalcDeadline(height uint64, sig string, accid uint64, nonce uint64) uint64 {
	x := C.calc_dl(C.ulonglong(height), C.CString(sig), C.ulonglong(accid), C.ulonglong(nonce))
	return uint64(x)
}

func WinTips(title string, msg string) {
	user32 := syscall.MustLoadDLL("user32.dll")
	// defer syscall.FreeLibrary(user32)
	MessageBox := user32.MustFindProc("MessageBoxW")
	MessageBox.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(msg))), // Text
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))), // Caption
		0x00000030)
}

func GetLogicalDrives() []string {
	kernel32 := syscall.MustLoadDLL("kernel32.dll")
	GetLogicalDrives := kernel32.MustFindProc("GetLogicalDrives")
	n, _, _ := GetLogicalDrives.Call()
	s := strconv.FormatInt(int64(n), 2)

	var drives_all = []string{"A:", "B:", "C:", "D:", "E:", "F:", "G:", "H:", "I:", "J:", "K:",
		"L:", "M:", "N:", "O:", "P:", "Q:", "R:", "S:", "T:",
		"U:", "V:", "W:", "X:", "Y:", "Z:"}
	temp := drives_all[0:len(s)]

	var d []string
	for i, v := range s {

		if v == 49 {
			l := len(s) - i - 1
			d = append(d, temp[l])
		}
	}

	var drives []string
	for i, v := range d {
		drives = append(drives[i:], append([]string{v}, drives[:i]...)...)
	}
	return drives
}

func get_P_size(fi os.FileInfo) (int64, bool) {
	// 要求单文件大于1M
	if fi.Size() < (1 << 20) {
		return 0, false
	}

	var accid, nonce, nonces, stagger uint64
	nitem, _ := fmt.Sscanf(fi.Name(), "%d_%d_%d_%d", &accid, &nonce, &nonces, &stagger)
	if (nitem == 3 && fmt.Sprintf("%d_%d_%d", accid, nonce, nonces) == fi.Name()) ||
		(nitem == 4 && fmt.Sprintf("%d_%d_%d_%d", accid, nonce, nonces, stagger) == fi.Name()) {
		// Todo: 打开看一下标志 :)
		size := fi.Size() >> 20
		return size, true
	}

	return 0, false
}

var PlistLocker *sync.Mutex = new(sync.Mutex)

func getPlist(deep int) (plist []Plist, err error) {
	PlistLocker.Lock()
	defer PlistLocker.Unlock()
	// 列举所有盘，遍历5层，找出大于500G的、名字可由_切分成三或四段的文件夹，取其文件大小
	var smap sync.Map
	drivers := GetLogicalDrives()
	oo.LogD("Walk dirvers: %v", drivers)

	for _, dv := range drivers {
		if dv == "C:" { // 不扫C盘
			continue
		}
		rootdir := dv + string(os.PathSeparator)
		filepath.Walk(rootdir, func(path string, f os.FileInfo, err error) error {
			if f == nil {
				return err
			}
			if f.IsDir() {
				dir := filepath.Dir(path)
				if len(strings.Split(dir, string(os.PathSeparator))) >= deep {
					return filepath.SkipDir
				}
				return nil
			}

			if size, ok := get_P_size(f); ok {
				dir := filepath.Dir(path)
				if oldsize, ok := smap.Load(dir); ok {
					size += oldsize.(int64)
				}
				// oo.LogD("Found %s : %d", dir, size)
				smap.Store(dir, size)
			}
			return nil
		})
	}

	smap.Range(func(key, value interface{}) bool {
		skey := strings.ToLower(key.(string))
		plist = append(plist, Plist{
			Path:     skey,
			Capacity: value.(int64),
			Flag:     1,
		})
		oo.LogD("FOUND DIR %s %d MB", skey, value.(int64))
		return true //continue
	})

	return plist, nil
}

// plist里有的，conf也有则是1. conf没则是0；plist里无而conf里有的，则是-1
func diffPlist(plist []Plist, Paths []string) (dstlist []Plist, err error) {
	leftPlist := []Plist{}
	for _, c := range Paths {
		c = strings.ToLower(c)

		found := -1
		for j, pl := range plist {
			if pl.Path == c {
				pl.Flag = 1
				dstlist = append(dstlist, pl)
				found = j
				break
			}
		}
		if found >= 0 {
			plist = append(plist[:found], plist[found+1:]...)
		} else {
			pl := Plist{Path: c, Capacity: 0, Flag: -1}
			leftPlist = append(leftPlist, pl)
		}
	}
	for _, pl := range plist {
		pl.Flag = 0
		dstlist = append(dstlist, pl)
	}
	dstlist = append(dstlist, leftPlist...)
	return
}

func (m *Miner) DelayScan() {
	// <-time.After(d)
	plist, _ := getPlist(4)
	// oo.LogD("plist: %v, Paths: %v", plist, conf.Paths)
	// 合并，plist里有的，conf也有则是1. conf没则是0；plist里无而conf里有的，则是-1
	dstlist, _ := diffPlist(plist, conf.Paths)
	oo.LogD("dstlist: %v", dstlist)

	wstr := ""
	for _, pl := range dstlist {
		if pl.Flag == 0 {
			if wstr != "" {
				wstr += ", "
			}
			wstr += fmt.Sprintf("%s", pl.Path)
		}
	}
	if wstr != "" {
		oo.LogD("Maybe missing config: %s", wstr)
		g_miner_status.WarningStr = fmt.Sprintf("Maybe missing config: %s", wstr)
	}
}

func (m *Miner) OnNewMiningInfo(minfo MiningInfo) {
	defer func() {
		if errs := recover(); errs != nil {
			oo.LogW("recover OnNewMiningInfo err=%v", errs)
		}
	}()
	// 确保停止老的协程
	ScheSetRunFlag(0)
	m.closedch <- struct{}{}

	oo.LogD("Start mining work")
	ScheSetRunFlag(1)

	// 读取盘
	for i, dir := range m.paths {
		go func(i int, dir string, minfo MiningInfo) {
			defer func() {
				if errs := recover(); errs != nil {
					oo.LogW("recover ScheRunNew: %s, err=%v", dir, errs)
				}
			}()
			if !m.is_not_first {
				H5Log("work", dir)
			}
			ScheRunNew(i, dir, uint64(minfo.Height),
				minfo.GenerationSignature,
				uint64(conf.TargetDeadline), uint64(minfo.BaseTarget))
			m.wait_chs <- i
		}(i, dir, minfo)
	}

	for i := 0; i < len(m.paths); i++ {
		<-m.wait_chs
		// oo.LogD("Wait one. i=%d, n=%d", i, n)
	}

	m.is_not_first = true
	oo.LogD("All work is done. ")
	<-m.closedch
}

func InitMiner(Paths []string, cache1 int64, cache2 int64, wakeup bool, inset string) *Miner {
	m := &Miner{}

	//打开所有文件，准备好缓存，待命
	m.paths = Paths

	m.closedch = make(chan struct{}, 1)
	m.wait_chs = make(chan int, len(m.paths))

	var wakeup1 int = 0
	if wakeup {
		wakeup1 = 1
	}

	// C.sche_init(C.int(len(Paths)), C.unsigned(cache1), C.unsigned(cache2), C.int(wakeup1), C.CString(inset))
	ScheInit(Paths, cache1, cache2, wakeup1, inset)

	if conf.ScanPlot {
		oo.LogD("Start ScanPlot...")
		go m.DelayScan()
	}

	// 接收log/scan/report, 晚一点起动最多就丢包，没关系
	go MinerMoniter()

	return m
}

func (m *Miner) Close() {
	ScheSetRunFlag(0)
	m.closedch <- struct{}{}
	ScheCleanup()

	close(m.wait_chs)
	close(m.closedch)
}

func (m *Miner) GetInset() string {
	return ScheGetInset()
}

func setStdHandle(stdhandle int32, handle syscall.Handle) error {
	kernel32 := syscall.MustLoadDLL("kernel32.dll")
	procSetStdHandle := kernel32.MustFindProc("SetStdHandle")

	r0, _, e1 := syscall.Syscall(procSetStdHandle.Addr(), 2, uintptr(stdhandle), uintptr(handle), 0)
	if r0 == 0 {
		if e1 != 0 {
			return error(e1)
		}
		return syscall.EINVAL
	}
	return nil
}

// redirectStderr to the file passed in
func redirectStderr(f *os.File) (err error) {
	err = setStdHandle(syscall.STD_ERROR_HANDLE, syscall.Handle(f.Fd()))
	if err == nil {
		os.Stderr = f
	}
	return
}
