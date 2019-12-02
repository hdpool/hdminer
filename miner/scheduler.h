#pragma once

typedef struct {
	int enable_avx512;
	int enable_avx256;
	int enable_avx128;
	int enable_sse;
	unsigned CPUInfo[32][4];
	char cpumark[32];
	char insets[256];
	int POC2;
	int wakeup;
	int first_cache;
} global_cfg_t;

extern global_cfg_t g_miner_cfg;

//目录，height, sig, basetarget, target_dl
void sche_runnew(int widx, const char *path_loc_str, unsigned long long height, 
	const char *signature, unsigned long long target_dl, unsigned long long basetarget);

void sche_init (/*char *paths[], */int npath, unsigned cache1, unsigned cache2, int wakeup, const char *inset);
void sche_cleanup ();

void sche_set_runflag (int runflag);

const char *sche_get_cpumark();

const char *sche_get_inset();

unsigned long long calc_dl (unsigned long long height, const char *signature, unsigned long long accid, unsigned long long nonce);

// void work_get_path_total_size()

enum {
	ATYPE_SCAN = 0,
	ATYPE_REPORT = 1,
	ATYPE_LOG = 2
};

#pragma pack(push, 1)
typedef struct {
	unsigned char len; //max: 256
	unsigned char type;
} head_arg_t;

typedef struct {
	unsigned long long bytes;
	// char dir[0]; //dyn len
} scan_arg_t;

typedef struct {
	char logtype[8];
	char logmsg[0]; //dyn len
} log_arg_t;

typedef struct {
	unsigned long long accid;
	unsigned long long height;
	unsigned long long nonce;
	unsigned long long dl;
} report_arg_t;
#pragma pack(pop)

void send_scan(int widx, unsigned long long bytes, const char *dir);
void send_report(unsigned long long accid, unsigned long long height, unsigned long long nonce, unsigned long long dl);
void send_log(const char *logtype, const char *fmt, ...);
