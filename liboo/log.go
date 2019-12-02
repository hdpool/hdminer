package oo

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
四种日志
1: 调试日志，输出到../log/debug.Date.log，只保留最多三天
2: 告警日志，输出到../log/warn.log
3: 状态日志，输出到../log/stat.log
4: 用户日志，输出到../log/u_XXX.log，其中XXX为uid
*/

type perfstat_t struct {
	val       int64
	total_val int64
}

type log_t struct {
	perfs    *sync.Map
	mdy_flag int // 0: 未动过，1：上次未动过，2：动过
	statf    *os.File
	warnf    *os.File
	logdir   string
	svrname  string
	logday   string // 20060102
	warnstr  string
	level    int
	uids     []uint64
	ufmap    *sync.Map
	errcb    func(s string)
}

var g_log = &log_t{}
var g_stdout = os.Stdout

func init() {
	g_log.logdir = "./"
	g_log.perfs = new(sync.Map)
	g_log.ufmap = new(sync.Map)
}

func keep_log_files(fdir string, pattern string, nfile int) {
	logfs := []string{}
	filepath.Walk(fdir, func(path string, f os.FileInfo, err error) error {
		if f == nil {
			return err
		}
		if f.IsDir() {
			if f.Name() == filepath.Base(fdir) {
				return nil
			}
			return filepath.SkipDir
		}
		if ok, _ := filepath.Match(pattern, f.Name()); ok {
			logfs = append(logfs, f.Name())
		}
		return nil
	})
	sort.Slice(logfs, func(i, j int) bool {
		return strings.Compare(logfs[i], logfs[j]) < 0
	})
	for i := 0; i < len(logfs)-nfile; i++ {
		fn := filepath.Join(fdir, logfs[i])
		os.Remove(fn)
	}
}

func InitLog(logdir string, svrname string, warnstr string, fn func(s string), intervals ...int64) {
	g_log.logdir = logdir
	g_log.svrname = svrname
	g_log.warnstr = warnstr
	g_log.errcb = fn

	chgf := func(t time.Time) {
		if t.Format("20060102") != g_log.logday {
			fname := filepath.Join(g_log.logdir, fmt.Sprintf("%s.%s.log", g_log.svrname, t.Format("20060102")))
			f, err := os.OpenFile(fname, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				return
			}
			g_log.logday = t.Format("20060102")
			tmpf := g_stdout
			g_stdout = f
			if tmpf != os.Stdout {
				tmpf.Close() // 最多就是出错
			}
			keep_log_files(g_log.logdir, fmt.Sprintf("%s.????????.log", g_log.svrname), 3)
		}
	}
	chgf(time.Now()) // 即时创建

	interval := int64(60)
	if len(intervals) > 0 {
		interval = intervals[0]
	}
	if interval > 0 {
		// 启动stat go
		go func() {
			for {
				tnow := time.Now()
				if tnow.Unix()%interval == 0 {
					logStat()
				}
				// 切换g_stdout
				chgf(tnow)

				time.Sleep(1 * time.Second)
			}
		}()
	}
}

func LogSetCallback(fn func(s string)) {
	g_log.errcb = fn
}

func LogUpdateLevel(level int, uids []uint64) {
	g_log.level = level
	g_log.uids = uids
}

func LogWrite(w io.Writer, level string, format string, v ...interface{}) {
	var nowTime string
	if level == "1" { // 仅debug日志不加时间
		nowTime = time.Now().Format("15:04:05.000") // "2006-01-02 15:04:05.000")
	} else {
		nowTime = time.Now().Format("2006-01-02 15:04:05.000")
	}
	_, file, line, _ := runtime.Caller(2)
	_, fileName := path.Split(file)

	msg := format
	if len(v) > 0 {
		msg = fmt.Sprintf(format, v...)
	}
	fmt.Fprintf(w, "[%s]%s<%s>: %s:%d %s\r\n", nowTime, g_log.svrname, level, fileName, line, strings.TrimRight(msg, "\r\n"))
}

func LogD(format string, v ...interface{}) {
	if g_log.level > 1 {
		return
	}
	LogWrite(g_stdout, "1", format, v...)
}

func LogW(format string, v ...interface{}) {
	LogWrite(g_stdout, "4", format, v...)

	if g_log.warnf == nil {
		f, err := os.OpenFile(g_log.logdir+"warn.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			fmt.Printf("err=%v\r\n", err)
			return
		}
		g_log.warnf = f
	}

	istr := fmt.Sprintf("\t%s\r\n", g_log.warnstr)
	for i := 1; ; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		_, fileName := path.Split(file)
		fn := runtime.FuncForPC(pc).Name()
		istr = istr + fmt.Sprintf("\t[-%d] %s(%s):%d\r\n", i, fileName, fn, line)
	}

	format = format + "\r\n%s"
	v = append(v, istr)

	LogWrite(g_log.warnf, "4", format, v...)

	if g_log.errcb != nil {
		sw := bytes.NewBufferString("")
		LogWrite(sw, "4", format, v...)
		go g_log.errcb(sw.String())
	}
}

func LogUid(uid uint64, format string, v ...interface{}) {
	if g_log.level <= 1 {
		LogWrite(g_stdout, fmt.Sprintf("u%d", uid), format, v...)
	}
	// save to u_xxx.log
	if InArray(uid, g_log.uids) {
		var f *os.File
		pf, loaded := g_log.ufmap.LoadOrStore(uid, &f)
		// fmt.Printf("f=%v, loaded=%v \r\n", &f, loaded)
		if loaded {
			ff, ok := pf.(**os.File)
			if !ok || *ff == nil {
				return
			}
			f = *ff
		} else {
			fname := fmt.Sprintf("%su_%d.log", g_log.logdir, uid)
			var err error
			f, err = os.OpenFile(fname, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0666)
			if err != nil {
				fmt.Printf("err=%v", err)
				g_log.ufmap.Delete(uid)
				return
			}
		}

		LogWrite(f, fmt.Sprintf("u%d", uid), format, v...)
	}
}

func LogBool(cond bool, args ...interface{}) {
	format := "SUCC: "
	if !cond {
		format = "FATAL: "
	}
	if len(args) > 0 {
		format += args[0].(string)
		args = args[1:]
	}
	LogWrite(g_stdout, "1", format, args...)
}

func LogError(err error, args ...interface{}) {
	format := "SUCC: "
	if err != nil {
		format = fmt.Sprintf("ERR %v", err)
	}
	if len(args) > 0 {
		format += args[0].(string)
		args = args[1:]
	}
	LogWrite(g_stdout, "1", format, args...)
}

func StatSet(key string, val int64) {
	g_log.mdy_flag = 2

	p, loaded := g_log.perfs.LoadOrStore(key, &perfstat_t{val, val})
	if loaded {
		pp, _ := p.(*perfstat_t)
		pp.val = val
		pp.total_val = val
	}
}
func StatChg(key string, val int64) {
	g_log.mdy_flag = 2

	p, loaded := g_log.perfs.LoadOrStore(key, &perfstat_t{val, val})
	if loaded {
		pp, _ := p.(*perfstat_t)
		pp.val += val
		pp.total_val += val
	}
}
func StatKeepMin(key string, val int64) {
	g_log.mdy_flag = 2

	p, loaded := g_log.perfs.LoadOrStore(key, &perfstat_t{val, val})
	if loaded {
		pp, _ := p.(*perfstat_t)
		if pp.val > val || pp.val == 0 {
			pp.val = val
			pp.total_val = val
		}
	}
}
func StatKeepMax(key string, val int64) {
	g_log.mdy_flag = 2

	p, loaded := g_log.perfs.LoadOrStore(key, &perfstat_t{val, val})
	if loaded {
		pp, _ := p.(*perfstat_t)
		if pp.val < val || pp.val == 0 {
			pp.val = val
			pp.total_val = val
		}
	}
}
func logStat() {
	if g_log.mdy_flag == 0 {
		return
	}

	var s string
	if g_log.mdy_flag == 2 {
		s = "\r\n"
		g_log.perfs.Range(func(key, val interface{}) bool {
			if v, ok := val.(*perfstat_t); ok {
				s = s + fmt.Sprintf("\t%v\t: %d / %d\r\n", key, v.val, v.total_val)
				v.val = 0
			}
			return true
		})

		g_log.mdy_flag = 1
	} else {
		s = "="
	}
	if len(s) == 0 {
		return
	}
	// save to stat.log
	if g_log.statf == nil {
		f, err := os.OpenFile(g_log.logdir+"stat.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			return
		}
		g_log.statf = f
	}
	LogWrite(g_log.statf, "s", s)
}
