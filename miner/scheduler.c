// #include "stdafx.h"
#ifdef WIN32
#include <winsock2.h>
#include <windows.h>
#else
#include <unistd.h>
#include <sys/types.h>
#include <sys/socket.h>
#include <arpa/inet.h>
#include <netinet/in.h>
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdarg.h>
#include <assert.h>
#include <x86intrin.h>
#include "scheduler.h"
#include "bfs.h"
#include "mshabal.h"
#include "sph_shabal.h"
#include "thread.h"
#include "rbuff.h"


size_t cache_size1 = 131072;			// Cache in nonces (1 nonce in scoop = 64 bytes) for native POC
size_t cache_size2 = 1024000;		// Cache in nonces (1 nonce in scoop = 64 bytes) for on-the-fly POC conversion
size_t readChunkSize = 131072;		// Size of HDD reads in nonces (1 nonce in scoop = 64 bytes)

typedef struct {
	#ifdef WIN32
	HANDLE ifile;
	#else
	int ifile;
	#endif
	unsigned long long  start;
	unsigned long long  MirrorStart;
	int *  cont;
	unsigned long long *  bytes;
	t_files  *  iter;
	int *  flip;
	int p2;
	unsigned long long  i;
	unsigned long long  stagger;
	size_t *  cache_size_local;
	char *  cache; //同一job内共享
	char *  MirrorCache; //同一worker共享

} read_arg_t;

//read与hash共用的参数，定长。
typedef struct {
	read_arg_t readarg;
	hash_arg_t hasharg;
	char *  cache; //注意这个不能复位
	size_t  cache_size_orig; //当前cache长
} jobarg_t; //sizeof < 256B

typedef struct {
	// thread_t *read_thd;
	thread_t *hash_thd;

	rbuff_t *idle_list; //read_thd在这等，初始化时要先推N个进来，读完给job_list
	rbuff_t *job_list; //hash_thd在这等，处理完还给idle_list
	// rbuff_t *scan_list; //init线程在这里等，各hash线程会写
	// rbuff_t *report_list;

	int widx;
	char path_loc_str[256]; //路径最多长256
	t_files  files[2048]; //本路径下文件， 每个申请248*2K=0.5M
	int nfiles; //文件数

	char *MirrorCache; //一个work只需要一个poc2 cache
	size_t MirrorCacheLen; //当前多长

	// char *first_cache; //第一块缓存出来
	// size_t first_cache_len; //
	// size_t first_cache_bytes; //第一块读出的字节数。
	// size_t first_cache_size_local; //第一块算出的字节数
} work_t;

#define NCACHE 3

// static thread_t *g_scan_thd;
// static thread_t *g_report_thd;

volatile int sche_runflag;

global_cfg_t g_miner_cfg;

work_t *g_wks[256];
int g_nwork;

static volatile unsigned long long s_height;
static volatile unsigned long long s_dl;

void (*th_hash)(hash_arg_t *parg);

static void udpsend (const char *buf, unsigned blen)
{
	static struct sockaddr_in sa={0};
	static volatile int g_udpfd = -1;

	if (g_udpfd == -1) {
		g_udpfd = socket (PF_INET, SOCK_DGRAM, 0);
		if (g_udpfd <= 0) {
			printf("Failed to create udpfd.\n");
			return;
		}
		unsigned rbsize = 1024*1024*1;
		int ret = setsockopt(g_udpfd, SOL_SOCKET, SO_SNDBUF, (char*)&rbsize, sizeof(rbsize));
		if (ret != 0) {
			printf("Failed to set fd sndbuf.\n");
		}

		sa.sin_family = PF_INET;
		sa.sin_port = htons(60100);
		sa.sin_addr.s_addr = inet_addr("127.0.0.1");
	}

	sendto (g_udpfd, buf, blen, 0, (struct sockaddr*)&sa, sizeof(sa));
}

void send_scan(int widx, unsigned long long bytes, const char *dir) {
	char buf[1024];
	head_arg_t *head = (head_arg_t*)buf;
	scan_arg_t *scan = (scan_arg_t*)(head+1);

	scan->bytes = bytes;
	head->type = ATYPE_SCAN;
	head->len = sizeof(head_arg_t)+sizeof(scan_arg_t);

	udpsend (buf, head->len);
}

void send_report(unsigned long long accid, unsigned long long height, unsigned long long nonce, unsigned long long dl) {
	char buf[1024];
	head_arg_t *head = (head_arg_t*)buf;

	report_arg_t *report = (report_arg_t*)(head+1);
	report->height = height;
	report->accid = accid;
	report->nonce = nonce;
	report->dl = dl;

	head->type = ATYPE_REPORT;
	head->len = sizeof(head_arg_t) + sizeof(report_arg_t);

	udpsend (buf, head->len);
}
void send_log(const char *logtype, const char *fmt, ...) {
	int off = 0, left = 255 - sizeof(head_arg_t)-sizeof(log_arg_t);
	va_list ap;
	char buf[1024];
	head_arg_t *head = (head_arg_t*)buf;

	log_arg_t *log = (log_arg_t*)(head+1);
	memset (log->logtype, 0, sizeof(logtype));
	strncpy (log->logtype, logtype, sizeof(log->logtype)-1);


	va_start (ap, fmt);
	off = vsnprintf (log->logmsg, left-1, fmt, ap);
	va_end (ap);
	log->logmsg[off++] = '\0';

	head->type = ATYPE_LOG;
	head->len = sizeof(head_arg_t) + sizeof(log_arg_t) + off;

	udpsend (buf, head->len);
}
void tools_sleep(int msec)
{
#ifdef WIN32
	Sleep(msec);
#else //win下要用真socket才有效
	struct timeval tv={0};
	tv.tv_sec = msec/1000;
	tv.tv_usec = (msec%1000)*1000;
	select(0,NULL,NULL,NULL,&tv);
#endif
}

// static void scan_thread(thread_t *thd) {
// 	int i, any;
// 	char tmpbuf[1024];
// 	head_arg_t *head = (head_arg_t*)tmpbuf;
// 	int nbyte;
// 	rbuff_t *b;

// 	while (!thread_testcancel (thd)) {
// 		any = sizeof(head_arg_t);
// 		for (i=0; i<g_nwork && any + 8 < sizeof(tmpbuf); i++) {
// 			if (g_wks[i]==NULL) {
// 				continue;
// 			}
// 			b = g_wks[i]->scan_list;
// 			nbyte = rbuff_datacount (b)/8 * 8;
// 			if (nbyte > sizeof(tmpbuf)-any) {
// 				nbyte = (sizeof(tmpbuf)-any)/8*8;
// 			}
// 			any += rbuff_get (b, tmpbuf+any, nbyte);
// 		}

// 		if (any > sizeof(head_arg_t)) {
// 			head->type = ATYPE_SCAN;
// 			head->len = any;
// 			udpsend (tmpbuf, head->len);
// 		} else { //没活干，休息一下
// 			tools_sleep (10);
// 		}
// 	}
// }

// Helper routines taken from http://stackoverflow.com/questions/1557400/hex-to-char-array-in-c
static int xdigit(char const digit) {
	int val;
	if ('0' <= digit && digit <= '9') val = digit - '0';
	else if ('a' <= digit && digit <= 'f') val = digit - 'a' + 10;
	else if ('A' <= digit && digit <= 'F') val = digit - 'A' + 10;
	else val = -1;
	return val;
}

static size_t xstr2strr(char *buf, size_t const bufsize, const char *const in) {
	if (!in) return 0; // missing input string

	size_t inlen = (size_t)strlen(in);
	if (inlen % 2 != 0) inlen--; // hex string must even sized

	size_t i, j;
	for (i = 0; i<inlen; i++)
		if (xdigit(in[i])<0) return 0; // bad character in hex string

	if (!buf || bufsize<inlen / 2 + 1) return 0; // no buffer or too small

	for (i = 0, j = 0; i<inlen; i += 2, j++)
		buf[j] = xdigit(in[i]) * 16 + xdigit(in[i + 1]);

	buf[inlen / 2] = '\0';
	return inlen / 2 + 1;
}

static unsigned int calc_scoop (const char *signature, unsigned long long height, sph_shabal_context *gcc) {
	char scoopgen[40];
	memmove(scoopgen, signature, 32);
	const char *mov = (char*)&height;
	sph_shabal_context local_32;
	memcpy(&local_32, gcc, sizeof(sph_shabal_context));
	scoopgen[32] = mov[7]; scoopgen[33] = mov[6]; scoopgen[34] = mov[5]; scoopgen[35] = mov[4]; scoopgen[36] = mov[3]; scoopgen[37] = mov[2]; scoopgen[38] = mov[1]; scoopgen[39] = mov[0];

	sph_shabal256(&local_32, (const unsigned char*)(const unsigned char*)scoopgen, 40);
	char xcache[32];
	sph_shabal256_close(&local_32, xcache);

	return (((unsigned char)xcache[31]) + 256 * (unsigned char)xcache[30]) % 4096;
}

#ifdef WIN32
static void th_read(read_arg_t *parg);

// VirtualAlloc(p, size, MEM_COMMIT, PAGE_READWRITE);
// VirtualFree (p, 0, MEM_RELEASE);

//一个目录下的所有文件
int GetFiles(const char *str, t_files p_files[], int nfiles)
{
	HANDLE hFile = INVALID_HANDLE_VALUE;
	WIN32_FIND_DATAA   FindFileData;
	int ret_n = 0;
	char tmpbuf[1024], tmp2[128];

	memset (p_files, 0, nfiles * sizeof(t_files));

	//load bfstoc
	if (strncmp(str, "\\\\.", 3) == 0) {
		const char *p = strchr (str+3, '+');
		if (p) {
			int len = p - str - 3;
			strncpy (tmpbuf, str+3, len);
			tmpbuf[len] = '\0';
			str = tmpbuf;
		}

		if (!LoadBFSTOC(str)) {
			return 0;
		}
		//iterate files
		int stop = 0;
		for (int i = 0; i < 72; i++) {
			if (stop) break;
			//check status
			switch (bfsTOC.plotFiles[i].status) {
			//no more files in TOC
			case 0:
				stop = 1;
				break;
			//finished plotfile
			case 1:
				strncpy (p_files[ret_n].Path, str, sizeof(p_files[0].Path)-1);
				p_files[ret_n].Path[sizeof(p_files[0].Path)-1] = '\0';
				sprintf (p_files[ret_n].Name, "FILE_%d", i);
				p_files[ret_n].Size = (unsigned long long)bfsTOC.plotFiles[i].nonces * 4096 *64;
				p_files[ret_n].Key = bfsTOC.id;
				p_files[ret_n].StartNonce = bfsTOC.plotFiles[i].startNonce;
				p_files[ret_n].Nonces = bfsTOC.plotFiles[i].nonces;
				p_files[ret_n].Stagger = bfsTOC.diskspace/64;
				p_files[ret_n].Offset = bfsTOC.plotFiles[i].startPos;
				p_files[ret_n].P2 = 1;
				p_files[ret_n].BFS = 1;

				ret_n++;
				break;
			//plotting in progress
			case 3:
				break;
			//other file status not supported for mining at the moment
			default:
				break;
			}
		}
		return ret_n;
	}

	snprintf(tmpbuf, sizeof(tmpbuf)-1, "%s\\*", str);
	tmpbuf[sizeof(tmpbuf)-1] = '\0';

	hFile = FindFirstFileA(tmpbuf, &FindFileData);
	if (INVALID_HANDLE_VALUE == hFile)
	{
		send_log("error", "Failed to find file %s, %d", tmpbuf, GetLastError());
		return 0;
	}

	do {
		if (FILE_ATTRIBUTE_DIRECTORY & FindFileData.dwFileAttributes) {
			continue; //Skip directories
		}

		unsigned long long key, nonce, nonces, stagger;
		int n = sscanf(FindFileData.cFileName, "%I64u_%I64u_%I64u_%I64u", &key, &nonce, &nonces, &stagger);
		if (n != 3 && n != 4) {
			continue;
		}
		memset (tmp2, 0, sizeof(tmp2));
		if (n==3) {
			snprintf (tmp2, sizeof(tmp2)-1, "%I64u_%I64u_%I64u", key, nonce, nonces);
		} else {
			snprintf (tmp2, sizeof(tmp2)-1, "%I64u_%I64u_%I64u_%I64u", key, nonce, nonces, stagger);
		}
		if (strcmp(tmp2, FindFileData.cFileName) != 0) {
			continue;
		}


		strncpy (p_files[ret_n].Path, str, sizeof(p_files[0].Path)-1);
		p_files[ret_n].Path[sizeof(p_files[0].Path)-1] = '\0';
		strncpy (p_files[ret_n].Name, FindFileData.cFileName, sizeof(p_files[0].Name)-1);
		p_files[ret_n].Name[sizeof(p_files[0].Name)-1] = '\0';
		p_files[ret_n].Size = (((((ULONGLONG)(FindFileData.nFileSizeHigh)) << (sizeof(FindFileData.nFileSizeLow) * 8)) | FindFileData.nFileSizeLow));
		p_files[ret_n].Key = key;
		p_files[ret_n].StartNonce = nonce;
		p_files[ret_n].Nonces = nonces;
		p_files[ret_n].Stagger = (n==3)? nonces : stagger;
		p_files[ret_n].Offset = 0;
		p_files[ret_n].P2 = (n==3);
		p_files[ret_n].BFS = 0;

		ret_n++;
	} while (FindNextFileA(hFile, &FindFileData) && ret_n < nfiles);
	FindClose(hFile);

	if (ret_n == nfiles) {
		send_log ("error", "%s more then %d files?", str, nfiles);
	}
	return ret_n;
}

//若预读，不需要再传dir了
void sche_runnew(int widx, const char *path_loc_str, unsigned long long height, 
	const char *signature, unsigned long long target_dl, unsigned long long basetarget)
{
	work_t *wk = g_wks[widx];
	char tmpbuf[1024];
	char real_signature[33];
	unsigned long long left_bytes = 0;
	// int is_first_block = g_miner_cfg.first_cache;

	size_t cache_size_local;
	unsigned long long real_cache_bytes;
	DWORD bytesPerSector;
	unsigned int scoop = 0;

	if (s_height != height) {
		s_height = height;
		s_dl = target_dl;
	}

	xstr2strr(real_signature, 33, signature);
	scoop = calc_scoop (real_signature, height, &global_32);

	if (wk==NULL) {
		printf("worker %d not ready.\n", widx);
		send_log ("error", "worker %d not ready dir: %s.\n", widx, path_loc_str);
		return;
	}

	if (wk->path_loc_str[0] == '\0' || g_miner_cfg.wakeup) {
		wk->nfiles = GetFiles (path_loc_str, wk->files, sizeof(wk->files)/sizeof(wk->files[0]));
		strncpy (wk->path_loc_str, path_loc_str, sizeof(wk->path_loc_str)-1);
		wk->path_loc_str[sizeof(wk->path_loc_str)-1] = '\0';
	}

	for (int j=0; j<wk->nfiles && sche_runflag; j++)
	{
		t_files *iter = &wk->files[j];
		unsigned long long nonce, nonces, stagger, offset;
		int p2, bfs;
		// key = iter->Key;
		nonce = iter->StartNonce;
		nonces = iter->Nonces;
		stagger = iter->Stagger;
		offset = iter->Offset;
		p2 = iter->P2;
		bfs = iter->BFS;

		// Проверка на повреждения плота
		if (nonces != (iter->Size) / (4096 * 64))
		{
			// bm_wprintw("file \"%s\" name/size mismatch\n", iter->Name.c_str(), 0);
			if (nonces != stagger)
				nonces = (((iter->Size) / (4096 * 64)) / stagger) * stagger; //обрезаем плот по размеру и стаггеру
			else
				if (scoop > (iter->Size) / (stagger * 64)) //если номер скупа попадает в поврежденный смерженный плот, то пропускаем
				{
					// bm_wprintw("skipped\n", 0);
					continue;
				}
		}

		//get sector information, set to 4096 for BFS
		if (!bfs) {
			DWORD sectorsPerCluster;
			DWORD numberOfFreeClusters;
			DWORD totalNumberOfClusters;
			if (!GetDiskFreeSpaceA(iter->Path, &sectorsPerCluster, &bytesPerSector, &numberOfFreeClusters, &totalNumberOfClusters))
			{
				send_log("error", "GetDiskFreeSpace failed\n"); //BFS
				continue;
			}
		}
		else
		{
			bytesPerSector = 4096;
		}

		// Если стаггер в плоте меньше чем размер сектора - пропускаем
		if ((stagger * 64) < bytesPerSector)
		{
			// bm_wprintw("stagger (%I64u) must be >= %I64u\n", stagger, bytesPerSector / 64, 0);
			continue;
		}

		// Если нонсов в плоте меньше чем размер сектора - пропускаем
		if ((nonces * 64) < bytesPerSector)
		{
			// bm_wprintw("nonces (%I64u) must be >= %I64u\n", nonces, bytesPerSector / 64, 0);
			continue;
		}

		// Если стаггер не выровнен по сектору - можем читать сдвигая последний стагер назад (доделать)
		if ((stagger % (bytesPerSector / 64)) != 0)
		{
			// bm_wprintw("stagger (%I64u) must be a multiple of %I64u\n", stagger, bytesPerSector / 64, 0);

			continue;
		}

		//PoC2 cache size added (shuffling needs more cache)
		if (p2 != g_miner_cfg.POC2) {
			if ((stagger == nonces) && (cache_size2 < stagger)) cache_size_local = cache_size2;  // оптимизированный плот
			else cache_size_local = stagger; // обычный плот
		}
		else {
			if ((stagger == nonces) && (cache_size1 < stagger))
			{
				cache_size_local = cache_size1;  // оптимизированный плот
			}
			else
			{
				if (!bfs) {
					cache_size_local = stagger; // обычный плот 
				}
				else
				{
					cache_size_local = cache_size1;  //BFS
				}
			}
		}

		cache_size_local = (cache_size_local / (size_t)(bytesPerSector / 64)) * (size_t)(bytesPerSector / 64);

		size_t cache_size_local_backup = cache_size_local;

		if (p2 != g_miner_cfg.POC2) {
			if (wk->MirrorCache == NULL || wk->MirrorCacheLen < cache_size_local*64) {
				wk->MirrorCache = (char *)realloc(wk->MirrorCache, cache_size_local * 64);
				wk->MirrorCacheLen = cache_size_local * 64;
			}
			if (wk->MirrorCache == NULL) {
				send_log ("error", "Failed to alloc memory: %d", cache_size_local*64);
				return;
			}
		}

		HANDLE ifile = INVALID_HANDLE_VALUE;
		if (bfs) {
			ifile = CreateFileA(iter->Path, GENERIC_READ, FILE_SHARE_READ, 0, OPEN_EXISTING, FILE_FLAG_NO_BUFFERING, NULL);
		} else {
			snprintf (tmpbuf, sizeof(tmpbuf)-1, "%s\\%s", iter->Path, iter->Name);
			ifile = CreateFileA(tmpbuf, GENERIC_READ, FILE_SHARE_READ, 0, OPEN_EXISTING, FILE_FLAG_NO_BUFFERING, NULL);
		}
		if (ifile == INVALID_HANDLE_VALUE) {
			send_log("error", "File \"%s\\%s\" error opening. code = %I64u\n", iter->Path, iter->Name, GetLastError());
			continue;
		}

		//PoC2 Vars
		unsigned long long MirrorStart;

		unsigned long long start, bytes;
		int i, flip = 0, cont = 0;
		unsigned blen=0;
		jobarg_t *pjob, job;
		pjob = &job;

		//nonces轮，每轮读min(nonces,stagger)个
		for (unsigned long long n = 0; n < nonces && sche_runflag; n += stagger)
		{
			cache_size_local = cache_size_local_backup;

			start = n * 4096 * 64 + scoop * stagger * 64 + offset * 64 * 64; //BFSstaggerstart + scoopstart + offset
			MirrorStart = n * 4096 * 64 + (4095 - scoop) * stagger * 64 + offset * 64 * 64; //PoC2 Seek possition

			for (i=0; i<min(nonces, stagger) && sche_runflag; i += cache_size_local) {

				//等待可用idle
				while (rbuff_datacount(wk->idle_list) == 0 && sche_runflag) {
					tools_sleep (10);
				}
				if (!sche_runflag) { //全放弃
					CloseHandle(ifile);
					return;
				}

				blen = rbuff_peek (wk->idle_list, pjob, sizeof(jobarg_t));
				if (blen < sizeof(jobarg_t)) {
					send_log ("error", "rbuff fail., len=%d<%d", blen, sizeof(jobarg_t));
					CloseHandle(ifile);
					return;
				}

				//确保cache是够用的
				real_cache_bytes = cache_size_local*64;
				if (pjob->cache==NULL || pjob->cache_size_orig < real_cache_bytes) {
					pjob->cache = (char *)realloc(pjob->cache, real_cache_bytes);
					pjob->cache_size_orig = real_cache_bytes;
				}
				if (pjob->cache == NULL) {
					send_log ("error", "Failed to alloc memory. size=%d", real_cache_bytes);
					CloseHandle(ifile);
					return;
				}

				pjob->readarg.ifile = ifile;
				pjob->readarg.start = start;
				pjob->readarg.MirrorStart = MirrorStart;
				pjob->readarg.cont = &cont;
				pjob->readarg.bytes = &bytes;
				pjob->readarg.iter = iter;
				pjob->readarg.flip = &flip;
				pjob->readarg.p2 = p2;
				pjob->readarg.i = i;
				pjob->readarg.stagger = stagger;
				pjob->readarg.cache_size_local = &cache_size_local;

				pjob->readarg.cache = pjob->cache;
				pjob->readarg.MirrorCache = wk->MirrorCache;

				th_read (&pjob->readarg);
				if (cont) { //定位文件指针出错了
					send_log ("error", "Failed to read. cont==true. file=%s", iter->Name);
					break;
				}

				if (!sche_runflag) { //读完回头看到已经退出了
					CloseHandle(ifile);
					return;
				}
				// rbuff_put (wk->scan_list, &bytes, sizeof(bytes));
				left_bytes += bytes; //4096
				if (left_bytes > 1024 * 32) { //1G发一次
					send_scan (widx, left_bytes, "");
					left_bytes = 0;
				}
				// printf ( "%s, Read bytes %I64u\n", path_loc_str, bytes);

				pjob->hasharg.accid = iter->Key;
				pjob->hasharg.height = height;
				memcpy(pjob->hasharg.signature, real_signature, 33);

				pjob->hasharg.basetarget = basetarget;
				pjob->hasharg.target_dl = s_dl; //会变，越找越好
				pjob->hasharg.best = 0; //默认未有
				pjob->hasharg.widx = widx;

				pjob->hasharg.iter = iter;
				pjob->hasharg.bytes = bytes;
				pjob->hasharg.cache_size_local = cache_size_local;
				pjob->hasharg.widx = widx;
				pjob->hasharg.nonce = n + nonce + i;// - cache_size_local;
				pjob->hasharg.n = n;
				pjob->hasharg.cache = pjob->readarg.cache;//pjob->cache;

				rbuff_put (wk->job_list, pjob, sizeof(jobarg_t));
				rbuff_erase (wk->idle_list, sizeof(jobarg_t));
			}
		}

		//bfs seek optimisation
		if (bfs && j+1 == wk->nfiles) {
			//assuming physical hard disk mid at scoop 1587
			LARGE_INTEGER liDistanceToMove = { 0 };
			SetFilePointerEx(ifile, liDistanceToMove, NULL, FILE_BEGIN);
		}
		CloseHandle(ifile);
	}
	if (left_bytes > 0) {
		send_scan (widx, left_bytes, "");
	}
	// send_log("log", "END %s height:%I64u target_dl:%I64u, basetarget:%I64u, total_bytes:%I64u, best DL: %I64u", 
	// 	path_loc_str, height, target_dl, basetarget, read_total_bytes, hasharg.best/basetarget);
	return;
}

static void th_read(read_arg_t *parg) {

	HANDLE ifile = parg->ifile;
	unsigned long long const start = parg->start;
	unsigned long long const MirrorStart = parg->MirrorStart;
	int * const cont = parg->cont;
	unsigned long long * const bytes = parg->bytes;

	t_files const * const iter = parg->iter;
	int * const flip = parg->flip;
	int p2 = parg->p2;
	unsigned long long const i = parg->i;
	unsigned long long const stagger = parg->stagger;

	size_t * const cache_size_local = parg->cache_size_local;
	char * const cache = parg->cache;
	char * const MirrorCache = parg->MirrorCache;

	if (i + *cache_size_local > stagger)
	{
		*cache_size_local = stagger - i;  // остаток

		if (g_miner_cfg.enable_avx512 && *cache_size_local < 16) {
			// bm_wprintw("WARNING: %I64u\n", *cache_size_local);
		}
		if (g_miner_cfg.enable_avx256 && *cache_size_local < 8) {
			// bm_wprintw("WARNING: %I64u\n", *cache_size_local);
		}
		if (g_miner_cfg.enable_avx128 && *cache_size_local < 4){
			// bm_wprintw("WARNING: %I64u\n", *cache_size_local);
		}
	}

	DWORD b = 0;
	DWORD Mirrorb = 0;
	LARGE_INTEGER liDistanceToMove;
	LARGE_INTEGER MirrorliDistanceToMove;

	//alternating front scoop back scoop
	if (*flip) goto poc2read;


	//POC1 scoop read
poc1read:
	*bytes = 0;
	b = 0;
	liDistanceToMove.QuadPart = start + i * 64;
	if (!SetFilePointerEx(ifile, liDistanceToMove, NULL, FILE_BEGIN))
	{
		send_log("error", "SetFilePointerEx. %s %s code = %I64u\n", iter->Path, iter->Name, GetLastError(), 0);
		*cont = 1;
		return;
	}
	do {
		if (!ReadFile(ifile, &cache[*bytes], (DWORD)(min(readChunkSize, *cache_size_local - *bytes / 64) * 64), &b, NULL))
		{
			send_log("error","P1 ReadFile %s %s. code = %I64u\n", iter->Path, iter->Name, GetLastError());
			break;
		}
		*bytes += b;

	} while (*bytes < *cache_size_local * 64);
	if (*flip) goto readend;

poc2read:
	//PoC2 mirror scoop read
	if (p2 != g_miner_cfg.POC2) {
		*bytes = 0;
		Mirrorb = 0;
		MirrorliDistanceToMove.QuadPart = MirrorStart + i * 64;
		if (!SetFilePointerEx(ifile, MirrorliDistanceToMove, NULL, FILE_BEGIN))
		{
			send_log("error", "SetFilePointerEx. %s %s code = %I64u\n", iter->Path, iter->Name, GetLastError());
			*cont = 1;
			return;
		}
		do {
			if (!ReadFile(ifile, &MirrorCache[*bytes], (DWORD)(min(readChunkSize, *cache_size_local - *bytes / 64) * 64), &Mirrorb, NULL))
			{
				send_log("error", "P2 ReadFile. %s %s code = %I64u\n", iter->Path, iter->Name, GetLastError());
				break;
			}
			*bytes += Mirrorb;
		} while (*bytes < *cache_size_local * 64);
	}
	if (*flip) goto poc1read;
readend:
	*flip = !*flip;

	//PoC2 Merge data to Cache
	if (p2 != g_miner_cfg.POC2) {
		for (unsigned long t = 0; t < *bytes; t += 64) {
			memcpy(&cache[t + 32], &MirrorCache[t + 32], 32); //copy second hash to correct place.
		}
	}
}
#else //!WIN32
void sche_runnew(int widx, const char *path_loc_str, unsigned long long height, 
	const char *signature, unsigned long long target_dl, unsigned long long basetarget){}
// static void th_read(read_arg_t *parg){}
#endif

static void hash_thread (thread_t *thd) {
	work_t *wk = (work_t*)thread_getarg (thd);
	jobarg_t *pjob, job;
	unsigned len = 0;

	pjob = &job;
	while (!thread_testcancel (thd)) {
		while (rbuff_datacount (wk->job_list) == 0 && sche_runflag) {
			tools_sleep (10);
		}
		if (!sche_runflag) {
			tools_sleep (10); //因为会过快continue浪费cpu
			//放弃所有中间缓冲，还给idle
			rbuff_move (wk->idle_list, wk->job_list, rbuff_datacount(wk->job_list));
			continue;
		}

		len = rbuff_get (wk->job_list, pjob, sizeof(jobarg_t));
		if (len != sizeof(jobarg_t)) {
			send_log ("error", "logic failed . when hash read. len=%u", len);
		}
		// pjob = (jobarg_t*)rbuff_get_read_ptr (wk->job_list, &len);

		//重新设置hash_arg ? 还是说在之前设好了比较合适。
		pjob->hasharg.target_dl = s_dl;
		th_hash (&pjob->hasharg);
		if (s_dl > pjob->hasharg.target_dl && pjob->hasharg.height == s_height) {
			s_dl = pjob->hasharg.target_dl;
		}

		//最终是要还的
		rbuff_put (wk->idle_list, pjob, sizeof(jobarg_t));
		// rbuff_erase (wk->job_list, sizeof(jobarg_t));
	}
	//统一归还idle
	rbuff_move (wk->idle_list, wk->job_list, rbuff_datacount(wk->job_list));
}

void work_cleanup(work_t *wk) {
	if (wk) {
		// if (wk->read_thd) {
		// 	thread_stop (wk->read_thd);
		// 	thread_destroy (wk->read_thd);
		// }
		if (wk->hash_thd) {
			thread_stop (wk->hash_thd);
			thread_destroy (wk->hash_thd);
		}

		if (wk->MirrorCache) {
			free (wk->MirrorCache);
		}
		// if (wk->first_cache) {
		// 	VirtualFree (wk->first_cache, 0, MEM_RELEASE);
		// }

		if (wk->idle_list) {
			//回收内存
			jobarg_t job;
			while(rbuff_get(wk->idle_list, &job, sizeof(jobarg_t)) == sizeof(jobarg_t)) {
				if (job.cache != NULL) {
					free (job.cache);
				}
			}

			rbuff_cleanup (wk->idle_list);
		}
		if (wk->job_list) {
			rbuff_cleanup (wk->job_list);
		}
		// if (wk->scan_list) {
		// 	rbuff_cleanup (wk->scan_list);
		// }
		// if (wk->report_list) {
		// 	rbuff_cleanup (wk->report_list);
		// }

		free (wk);
	}
}
work_t *work_create (int widx, const char *dir) {
	unsigned blen;
	jobarg_t *pjarg;
	work_t *wk = (work_t*)calloc(1, sizeof(work_t));
	if (!wk) {
		return NULL;
	}
	wk->widx = widx;
	// strncpy (wk->path_loc_str, dir, sizeof(wk->path_loc_str)-1);

	// //扫描目录
	// wk->nfiles = GetFiles(dir, wk->files, sizeof(wk->files)/sizeof(wk->files[0]));
	// if (wk->nfiles == 0) {
	// 	//没有文件，其它不用创建
	// 	return wk;
	// }

	//创建缓冲
	wk->idle_list = rbuff_init (NULL, sizeof(jobarg_t) * NCACHE);
	wk->job_list = rbuff_init (NULL, sizeof(jobarg_t) * NCACHE);
	// wk->scan_list = rbuff_init (NULL, sizeof(unsigned long long) * 4096);
	// wk->report_list = rbuff_init (NULL, sizeof(report_arg_t)*128);
	if (wk->idle_list==NULL || wk->job_list==NULL) {
		goto _Failed;
	}

	//预建内存
	blen = sizeof(jobarg_t)*NCACHE;
	pjarg = (jobarg_t*)rbuff_get_write_ptr (wk->idle_list, &blen);
	assert (blen == sizeof(jobarg_t)*NCACHE);

	memset (pjarg, 0, sizeof(jobarg_t)*NCACHE);

	rbuff_step_write_ptr (wk->idle_list, blen);

	//创建线程，将死等作业//小心，开始时要先确认其余成员是否就绪
	// wk->read_thd = thread_create (read_thd, wk);
	wk->hash_thd = thread_create (hash_thread, wk);
	if (wk->hash_thd == NULL) {
		goto _Failed;
	}

	return wk;

_Failed:
	work_cleanup (wk);
	return NULL;
}
static void create_shabal_ctx (const char *inset) {

	int off=strlen(g_miner_cfg.insets);
	//未设置，且支持，且未指定或或指定为这类
#ifdef AVX512F
	if (!th_hash && g_miner_cfg.enable_avx512 && (inset[0]=='\0' || strcmp(inset, "AVX512") == 0)) {
		send_log("HASH", "AVX512");
		simd512_mshabal_init(&global_512, 256);
		//create minimal context
		global_512_fast.out_size = global_512.out_size;
		for (int j = 0;j<704;j++) {
			global_512_fast.state[j] = global_512.state[j];
		}
		global_512_fast.Whigh = global_512.Whigh;
		global_512_fast.Wlow = global_512.Wlow;

		th_hash = procscoop_avx512_fast;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "-AVX512");
	}
#endif
	if (!th_hash && g_miner_cfg.enable_avx256 && (inset[0]=='\0' || strcmp(inset, "AVX2") == 0)) {
		send_log("HASH", "AVX2");
		simd256_mshabal_init(&global_256, 256);
		//create minimal context
		global_256_fast.out_size = global_256.out_size;
		for (int j = 0;j<352;j++) {
			global_256_fast.state[j] = global_256.state[j];
		}
		global_256_fast.Whigh = global_256.Whigh;
		global_256_fast.Wlow = global_256.Wlow;

		th_hash = procscoop_avx2_fast;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "-AVX2");
	} 

	if (!th_hash && g_miner_cfg.enable_avx128 && (inset[0]=='\0' || strcmp(inset, "AVX") == 0)) {
		send_log("HASH", "AVX");
		simd128_mshabal_init(&global_128, 256);
		//create minimal context
		global_128_fast.out_size = global_128.out_size;
		for (int j = 0;j<176;j++) {
			global_128_fast.state[j] = global_128.state[j];
		}
		global_128_fast.Whigh = global_128.Whigh;
		global_128_fast.Wlow = global_128.Wlow;

		th_hash = procscoop_avx_fast;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "-AVX");
	} 

	if (!th_hash && g_miner_cfg.enable_sse && (inset[0]=='\0' || strcmp(inset, "SSE") == 0)) {
		send_log("HASH", "SSE");
	    simd128_sse2_mshabal_init(&global_128, 256);
		// simd128_mshabal_init(&global_128, 256);
	    global_128_fast.out_size = global_128.out_size;
	    for (int i = 0; i < 176; i++) {
	    	global_128_fast.state[i] = global_128.state[i];
	    }
	    global_128_fast.Whigh = global_128.Whigh;
	    global_128_fast.Wlow = global_128.Wlow;

		th_hash = procscoop_sse_fast;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "-SSE");
	}

	sph_shabal256_init (&global_32);

	if (!th_hash || strcmp(inset, "DEFAULT")==0) { //未设置或强制指定
		send_log("HASH", "DEFAULT");
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "-DEFAULT");
		th_hash = procscoop_sph;
	}

	g_miner_cfg.insets[off] = '\0';
}

#include <stdint.h>
void run_cpuid(uint32_t eax, uint32_t ecx, uint32_t* abcd)
{
#if defined(_MSC_VER)
  __cpuidex(abcd, eax, ecx);
#else
  uint32_t ebx=0, edx=0;
 #if defined( __i386__ ) && defined ( __PIC__ )
  /* in case of PIC under 32-bit EBX cannot be clobbered */
  __asm__ ( "movl %%ebx, %%edi \n\t cpuid \n\t xchgl %%ebx, %%edi" : "=D" (ebx),
 # else
  __asm__ ( "cpuid" : "+b" (ebx),
 # endif
		      "+a" (eax), "+c" (ecx), "=d" (edx) );
	    abcd[0] = eax; abcd[1] = ebx; abcd[2] = ecx; abcd[3] = edx;
#endif
}     
   
int check_xcr0_zmm() {
  uint32_t xcr0;
  uint32_t zmm_ymm_xmm = (7 << 5) | (1 << 2) | (1 << 1);
#if defined(_MSC_VER)
  xcr0 = (uint32_t)_xgetbv(0);  /* min VS2010 SP1 compiler is required */
#else
  __asm__ ("xgetbv" : "=a" (xcr0) : "c" (0) : "%edx" );
#endif
  return ((xcr0 & zmm_ymm_xmm) == zmm_ymm_xmm); /* check if xmm, zmm and zmm state are enabled in XCR0 */
}

int has_intel_knl_features() {
  uint32_t abcd[4];
  uint32_t osxsave_mask = (1 << 27); // OSX.
  // uint32_t avx2_bmi12_mask = (1 << 16) | // AVX-512F
  //                            (1 << 26) | // AVX-512PF
  //                            (1 << 27) | // AVX-512ER
  //                            (1 << 28);  // AVX-512CD
  run_cpuid( 1, 0, abcd );
  // step 1 - must ensure OS supports extended processor state management
  if ( (abcd[2] & osxsave_mask) != osxsave_mask ) 
    return 0;
  // step 2 - must ensure OS supports ZMM registers (and YMM, and XMM)
  if ( ! check_xcr0_zmm() )
    return 0;
   
  return 1;
}

void cpu_init () {
#ifdef WIN32
	int i, nid = 0;
	unsigned f_1_EAX_=0, /*f_1_EBX_=0, f_1_ECX_ = 0, */f_1_EDX_ = 0;
	unsigned /*f_3_EAX_=0, f_3_EBX_=0, */f_3_ECX_ = 0, f_3_EDX_ = 0;
	unsigned f_7_EBX_ = 0;//, f_7_ECX_ = 0;

	// __cpuid (g_miner_cfg.CPUInfo[0], 0);
	run_cpuid (0, 0, g_miner_cfg.CPUInfo[0]);
	nid = g_miner_cfg.CPUInfo[0][0];

	for (i=0; i<nid && i<sizeof(g_miner_cfg.CPUInfo)/sizeof(g_miner_cfg.CPUInfo[0]); i++) {
		// __cpuidex (g_miner_cfg.CPUInfo[i], i, 0);
		run_cpuid (i, 0, g_miner_cfg.CPUInfo[i]);

		send_log("log", "CPUID %d: %X %X %X %X", i, g_miner_cfg.CPUInfo[i][0], g_miner_cfg.CPUInfo[i][1], 
			g_miner_cfg.CPUInfo[i][2], g_miner_cfg.CPUInfo[i][3]);
	}
 
	if (nid >= 1) {
		f_1_EAX_ = g_miner_cfg.CPUInfo[1][0];
		// f_1_EBX_ = g_miner_cfg.CPUInfo[1][1];
		// f_1_ECX_ = g_miner_cfg.CPUInfo[1][2];
		f_1_EDX_ = g_miner_cfg.CPUInfo[1][3];
	}

	if (nid >= 3) {
		// f_3_EAX_ = g_miner_cfg.CPUInfo[3][0];
		// f_3_EBX_ = g_miner_cfg.CPUInfo[3][1];
		f_3_ECX_ = g_miner_cfg.CPUInfo[3][2];
		f_3_EDX_ = g_miner_cfg.CPUInfo[3][3];
	}

	if (nid >= 7) {
		f_7_EBX_ = g_miner_cfg.CPUInfo[7][1];
		// f_7_ECX_ = g_miner_cfg.CPUInfo[7][2];
	}

	__builtin_cpu_init ();

	// Checking for AVX requires 3 things:
	// 1) CPUID indicates that the OS uses XSAVE and XRSTORE instructions (allowing saving YMM registers on context switch)
	// 2) CPUID indicates support for AVX
	// 3) XGETBV indicates the AVX registers will be saved and restored on context switch
	// int avxSupported = 0;
	// if (g_miner_cfg.CPUInfo[1][2] & (1 << 27) && g_miner_cfg.CPUInfo[1][2] & (1 << 28)) {
	// 	// Check if the OS will save the YMM registers
	// 	avxSupported = (_xgetbv(_XCR_XFEATURE_ENABLED_MASK) & 0x6) == 0x6;
	// }
	// if (!avxSupported) {
	// 	send_log ("error", "Not support AVX.");
	// }

	int off = 0;
	// g_miner_cfg.enable_avx512 = has_intel_knl_features();
	// if (g_miner_cfg.enable_avx512) {
	if (f_7_EBX_ & (1<<16)) {
		g_miner_cfg.enable_avx512 = 1;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "%sAVX512", off>0? ",":"");
	}
	// if (f_7_EBX_ & (1<<5)) {
	if (__builtin_cpu_supports ("avx2")) {
		g_miner_cfg.enable_avx256 = 1;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "%sAVX2", off>0? ",":"");
	}
	// if (f_1_ECX_ & (1<<28)) {
	if (__builtin_cpu_supports ("avx")) {
		g_miner_cfg.enable_avx128 = 1;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "%sAVX", off>0? ",":"");
	}
	// if (f_1_EDX_ & (1<<25)) {
	if (__builtin_cpu_supports ("sse2")) {
		// DISABLE SSE !!!
		g_miner_cfg.enable_sse = 1;
		off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "%sSSE", off>0? ",":"");
	}

	off += snprintf (g_miner_cfg.insets+off, sizeof(g_miner_cfg.insets)-off, "%sDEFAULT", off>0? ",":"");

	g_miner_cfg.insets[off] = '\0';

	snprintf(g_miner_cfg.cpumark, sizeof(g_miner_cfg.cpumark)-1, "%X%X%X%X", 
		f_1_EAX_, f_1_EDX_, f_3_ECX_, f_3_EDX_);
#endif
}

void sche_init (int npath, unsigned cache1, unsigned cache2, int wakeup, const char *inset)
{
	int i;

	if (cache1 > 0) {
		cache_size1 = cache1;
		readChunkSize = cache1;
	}
	if (cache2 > 0)	{
		cache_size2 = cache2;
	}
	memset (&g_miner_cfg, 0, sizeof(g_miner_cfg));

	cpu_init ();
	printf("Cpu avx512:%d, avx2:%d, avx:%d, sse2:%d\n", g_miner_cfg.enable_avx512, 
		g_miner_cfg.enable_avx256,g_miner_cfg.enable_avx128, g_miner_cfg.enable_sse);
	create_shabal_ctx (inset);

	g_miner_cfg.POC2 = 1;
	g_miner_cfg.wakeup = wakeup;
	// g_miner_cfg.first_cache = first_cache;

	for (i = 0; i < npath; ++i) {
		//每人10k
		// g_rbuff[i] = rbuff_init (NULL, 10240);
		g_wks[i] = work_create (i, "");//paths[i]);
		if (g_wks[i] == NULL) {
			printf("Error : create worker %d\n", i);
			send_log ("error", "Failed to create worker %d", i);
		}
	}
	g_nwork = npath;

	//这时才可以创建init线程
	// g_scan_thd = thread_create (scan_thread, NULL);
	// g_report_thd = thread_create (report_thd, NULL);
	send_log ("log","succeed to init.");
}

void sche_set_runflag (int runflag)
{
	sche_runflag = runflag;
}
void sche_cleanup ()
{
	// if (g_scan_thd) {
	// 	thread_stop (g_scan_thd);
	// 	thread_destroy (g_scan_thd);
	// 	g_scan_thd = NULL;
	// }
	// if (g_report_thd) {
	// 	thread_stop (g_report_thd);
	// 	thread_destroy (g_report_thd);
	// 	g_report_thd = NULL;
	// }
   
	for (int i=0; i<g_nwork; i++) {
		work_cleanup(g_wks[i]);
		g_wks[i] = NULL;
	}

	g_nwork = 0;
}
const char *sche_get_cpumark()
{
	return g_miner_cfg.cpumark;
}

const char *sche_get_inset()
{
	return g_miner_cfg.insets;
}

#define SCOOP_SIZE 64
#define NUM_SCOOPS 4096
#define NONCE_SIZE (NUM_SCOOPS * SCOOP_SIZE)

#define HASH_SIZE 32
#define HASH_CAP 4096

#define SSE4_PARALLEL 4
#define AVX2_PARALLEL 8

#define SET_NONCE(gendata, nonce, offset)                                      \
  xv = (char *)&nonce;                                                         \
  gendata[NONCE_SIZE + offset] = xv[7];                                        \
  gendata[NONCE_SIZE + offset + 1] = xv[6];                                    \
  gendata[NONCE_SIZE + offset + 2] = xv[5];                                    \
  gendata[NONCE_SIZE + offset + 3] = xv[4];                                    \
  gendata[NONCE_SIZE + offset + 4] = xv[3];                                    \
  gendata[NONCE_SIZE + offset + 5] = xv[2];                                    \
  gendata[NONCE_SIZE + offset + 6] = xv[1];                                    \
  gendata[NONCE_SIZE + offset + 7] = xv[0]

unsigned long long calc_dl(unsigned long long height, const char *signature, unsigned long long accid, unsigned long long nonce) {
  	unsigned int scoop_nr = 0;
  	char sig[32 + 128];
  	xstr2strr(sig, 33, signature);
  	sph_shabal_context save_32;
  	sph_shabal256_init(&save_32);
  	scoop_nr = calc_scoop (sig, height, &save_32);

  	char final[32];
  	char gendata[16 + NONCE_SIZE];
  	char *xv;

  	SET_NONCE(gendata, accid, 0);
  	SET_NONCE(gendata, nonce, 8);

  	sph_shabal_context x;
  	int len;

  	for (int i = NONCE_SIZE; i > 0; i -= HASH_SIZE) {
    // sph_shabal256_init(&x);
  		memcpy(&x, &save_32, sizeof(save_32));

  		len = NONCE_SIZE + 16 - i;
  		if (len > HASH_CAP)
  			len = HASH_CAP;

  		sph_shabal256(&x, (unsigned char *)&gendata[i], len);
  		sph_shabal256_close(&x, &gendata[i - HASH_SIZE]);
  	}

  //sph_shabal256_init(&x);
  	memcpy(&x, &save_32, sizeof(save_32));
  	sph_shabal256(&x, (unsigned char *)gendata, 16 + NONCE_SIZE);
  	sph_shabal256_close(&x, final);

  // XOR with final
  	for (int i = 0; i < NONCE_SIZE; i++)
  		gendata[i] ^= (final[i % 32]);

  	sph_shabal_context deadline_sc;
  // sph_shabal256_init(&deadline_sc);
  	memcpy(&deadline_sc, &save_32, sizeof(save_32));
  	sph_shabal256(&deadline_sc, (const unsigned char *)sig, HASH_SIZE);

  	uint8_t scoop[SCOOP_SIZE];
  	memcpy(scoop, gendata + (scoop_nr * SCOOP_SIZE), 32);
  	memcpy(scoop + 32, gendata + ((4095 - scoop_nr) * SCOOP_SIZE) + 32, 32);

  	uint8_t finals2[HASH_SIZE];
  	sph_shabal256(&deadline_sc, scoop, SCOOP_SIZE);
  	sph_shabal256_close(&deadline_sc, (uint32_t *)finals2);
  	unsigned long long *finals3 = (unsigned long long*)finals2;
  // req->deadline = *(uint64_t *)finals2 / req->base_target;
  	return *finals3;
}
