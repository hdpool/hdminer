package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"github.com/hdpool/hdminer/liboo"
)

/*
//@ c lang
#include <stdio.h>
#include <stdlib.h>
//#include <malloc.h>

#pragma pack(push, 1)
struct BFSPlotFile
{
	unsigned long long startNonce;
	unsigned int nonces;
	unsigned long long startPos;
	unsigned int status;
	unsigned int pos;
}; //sizeof=28
struct BFSTOC
{
	char version[4];
	unsigned int crc32;
	unsigned long long diskspace;
	unsigned long long id;
	unsigned long long reserved1;
	struct BFSPlotFile plotFiles[72];
	char reserved2[2048];
}; //sizeof=4096
#pragma pack(pop)

unsigned long long getBfsFileSize(char* path){
	struct BFSTOC   bfsTOC;                    // BFS Table of Contents
	unsigned int 	bfsTOCOffset = 5;          // 4k address of BFSTOC on harddisk. default = 5
    unsigned long long len = 0;

    FILE *fp = fopen(path,"rb");
    if (NULL == fp){
        return len;
    }
	if (-1 == fseek(fp, bfsTOCOffset * 4096, SEEK_SET)){
		fclose(fp);
		return len;
	}
	if (1 != fread(&bfsTOC, sizeof(bfsTOC), 1, fp)){
		fclose(fp);
		return len;
	}
	fclose(fp);

	//check if red content is really a BFSTOC
	char bfsversion[4] = { 'B', 'F', 'S', '1' };

	if (*bfsTOC.version != *bfsversion) {
		return len;
	}

	int stop = 0;
    int i = 0;
	for (i = 0; i < 72; i++) {
		if (stop) break;
		//check status
		switch (bfsTOC.plotFiles[i].status) {
		case 0: 	//no more files in TOC
			stop = 1;
			break;
		case 1: 	//finished plotfile
			len += (unsigned long long)bfsTOC.plotFiles[i].nonces * 4096 *64;
			break;
		case 3: 	//plotting in progress
			break;
		default: 	//other file status not supported for mining at the moment
			break;
		}
	}

    return len;
}

#cgo LDFLAGS:
*/
import "C"

type PlotFileType struct {
	FileName   string // 文件名是重要因素，没法通过来做链接作假
	FileSize   uint64
	AccountId  uint64
	StartNonce uint64
	Nonces     uint64
	Stagger    uint64
}

type AllPlotPathType struct {
	AllPaths  map[string]uint64       // 存储可能重复的目录=>Size
	AllFiles  map[string]PlotFileType // 存储可能重复的文件名=>File
	AllSize   uint64
	AllNonces uint64
	AllAccIds map[uint64]bool // 存储可能重复的accid
}

var GAllPlotPaths AllPlotPathType

func getMineCapacity(paths []string) (allPlotPaths AllPlotPathType, capacity uint64, err error) {
	allPlot := AllPlotPathType{
		AllPaths:  make(map[string]uint64),
		AllFiles:  make(map[string]PlotFileType),
		AllAccIds: make(map[uint64]bool),
	}

	for _, dirname := range paths {
		// bfs
		if strings.HasPrefix(dirname, "\\\\") || strings.HasSuffix(dirname, ".bfs") {
			if _, ok := allPlot.AllPaths[dirname]; ok {
				oo.LogD("Skip path:", dirname)
				continue
			}
			cs := C.CString(dirname)
			allPlot.AllPaths[dirname] = uint64(C.getBfsFileSize(cs))
			C.free(unsafe.Pointer(cs))
			continue
		}
		// 正常文件夹
		absname := dirname
		if !path.IsAbs(absname) {
			absname, err = filepath.Abs(absname)
			if nil != err {
				oo.LogD("Failed to abs file: %s, err=%v", absname, err)
				continue
			}
		}
		if _, ok := allPlot.AllPaths[absname]; ok {
			oo.LogD("Skip path: %s=>%s", dirname, absname)
			continue // 已经有过了，忽略
		}

		fis, err := ioutil.ReadDir(absname)
		if nil != err {
			oo.LogD("%v", err)
			continue
		}
		for _, fi := range fis {
			if !fi.Mode().IsRegular() { // 必须是常规文件
				continue
			}

			filename := absname + string(os.PathSeparator) + fi.Name()
			if _, ok := allPlot.AllFiles[filename]; ok {
				oo.LogD("Skip file: %s: %s", absname, fi.Name())
				continue
			}
			var accid, nonce, nonces, stagger uint64
			nitem, err := fmt.Sscanf(fi.Name(), "%d_%d_%d_%d", &accid, &nonce, &nonces, &stagger)
			if (nitem == 3 && fmt.Sprintf("%d_%d_%d", accid, nonce, nonces) == fi.Name()) ||
				(nitem == 4 && fmt.Sprintf("%d_%d_%d_%d", accid, nonce, nonces, stagger) == fi.Name()) {

				allPlot.AllNonces += nonces
				allPlot.AllAccIds[accid] = true
				allPlot.AllSize += uint64(fi.Size())
				allPlot.AllPaths[absname] += uint64(fi.Size())

				allPlot.AllFiles[filename] = PlotFileType{
					FileName:   fi.Name(),
					FileSize:   uint64(fi.Size()),
					AccountId:  accid,
					StartNonce: nonce,
					Nonces:     nonces,
					Stagger:    stagger,
				}
				// oo.LogD("load file success %s/%s", dirname, fi.Name())
			} else {
				oo.LogD("load file error path[%s] file[%s], err=%v", dirname, fi.Name(), err)
			}
		}
	}

	// capacity = allPlot.AllSize
	// for _, val := range data {
	// 	capacity += val
	// }

	// capacity >>= 30

	return allPlot, allPlot.AllSize, nil
}

func openUrlWithDefaultBrowser(url string) (err error) {
	var commands = map[string]string{
		"windows": "explorer",
		"darwin":  "open",
		"linux":   "xdg-open",
	}

	run, ok := commands[runtime.GOOS]
	if !ok {
		return fmt.Errorf("don't know how to open things on %s platform", runtime.GOOS)
	}

	cmd := exec.Command(run, url)

	return cmd.Start()
}
