package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/json-iterator/go"
)

type MinerConf struct {
	Server         string   `json:"Server"`
	Port           int64    `json:"Port"`
	TargetDeadline int64    `json:"TargetDeadline"`
	Paths          []string `json:"Paths"`
	CacheSize      int64    `json:"CacheSize"`
	CacheSize2     int64    `json:"CacheSize2"`
	UseHDDWakeUp   bool     `json:"UseHDDWakeUp"`
	Capacity       int64    `json:"Capacity"`
	CapBytes       int64
	RealPaths      []string // 去掉无用目录后的
}

type HdpoolConf struct {
	Version       string   `json:"Version"`    // by local
	VersionUrl    string   `json:"VersionUrl"` // skip
	LinkPoint     string   `json:"LinkPoint"`
	AccountKey    string   `json:"AccountKey"`
	MinerName     string   `json:"MinerName"`
	MinerExe      string   `json:"MinerExe"`
	AutoUpdate    int64    `json:"AutoUpdate"`  // skip
	AutoStartup   int64    `json:"AutoStartup"` // skip
	OpenWeb       bool     `json:"OpenWeb"`
	OpenScan      bool     `json:"OpenScan"`
	ScanPlot      bool     `json:"ScanPlot"`
	Inset         string   `json:"Inset"`
	Coins         []string `json:"Coins"`
	RescanRate    int64    `json:"RescanRate"`    // 内挖模式下，被中断时扫到少于百分之几，会回归重扫. 默认值70
	SubmitTimeout int64    `json:"SubmitTimeout"` // 多少秒没提交就认为没在工作, 可以切换币种。默认8秒
}

type ClientConfig struct {
	MinerConf
	HdpoolConf
}

var (
	conf      ClientConfig
	blago_cfg = "miner.conf"

	MODE_EMBEDDED     = "embedded"     // hdminer mode
	MODE_SINGLE_PROXY = "single_proxy" // hdproxy mode
	MODE_CENTER_PROXY = "center_proxy" // center proxy mode
)

func loadConfig() (err error) {
	// 预设值
	conf.OpenScan = true
	conf.OpenWeb = true
	conf.Version = GVersion

	buf, err := ioutil.ReadFile(blago_cfg)
	if nil != err {
		err = fmt.Errorf("read miner.conf err %v", err)
		return
	}
	err = jsoniter.Unmarshal(buf, &conf)
	if nil != err {
		err = fmt.Errorf("miner.conf parse json err %v", err)
		return
	}

	if "" == conf.MinerName {
		conf.MinerName, _ = os.Hostname()
		if nil != err {
			err = fmt.Errorf("get hostname err %v", err)
			return
		}
	}
	if "" == conf.LinkPoint {
		err = fmt.Errorf("LinkPoint is empty %v", err)
		return
	}
	if "" == conf.AccountKey {
		err = fmt.Errorf("AccountKey is empty")
		return
	}
	if 0 == conf.Port {
		err = fmt.Errorf("hdpool-guard listening port not set")
		return
	}
	if 0 == len(conf.Paths) {
		err = fmt.Errorf("Paths is empty")
		return
	}

	var capacity uint64
	GAllPlotPaths, capacity, err = getMineCapacity(conf.Paths)
	if nil != err {
		return
	}

	for p, size := range GAllPlotPaths.AllPaths {
		_ = size
		conf.RealPaths = append(conf.RealPaths, p)
	}

	conf.CapBytes = int64(capacity)
	conf.Capacity = conf.CapBytes >> 30

	// 根据程序名，确定运行模式
	conf.MinerExe, actor = MODE_SINGLE_PROXY, NewActorProxy()
	if strings.Contains(GServerName, "miner") {
		conf.MinerExe, actor = MODE_EMBEDDED, NewActorMiner()
	} else if strings.Contains(GServerName, "center_proxy") {
		conf.MinerExe, actor = MODE_CENTER_PROXY, NewActorCenterProxy()
	}

	if len(conf.Coins) == 0 {
		conf.Coins = []string{"BHD"}
	}

	conf.RescanRate = conf.RescanRate * 100
	if conf.RescanRate <= 0 {
		conf.RescanRate = 7000
	} else if conf.RescanRate >= 10000 {
		conf.RescanRate = 10000
	}

	if conf.SubmitTimeout <= 0 {
		conf.SubmitTimeout = 8
	}

	return
}
