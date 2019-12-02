#pragma once

#pragma pack(1)
typedef struct
{
	unsigned long long startNonce;
	unsigned int nonces;
	unsigned long long startPos;
	unsigned int status;
	unsigned int pos;
} BFSPlotFile;
#pragma pack()

#pragma pack(1)
typedef struct 
{
	char version[4];
	unsigned int crc32;
	unsigned long long diskspace;
	unsigned long long id;
	unsigned long long reserved1;
	BFSPlotFile plotFiles[72];
	char reserved2[2048];	
} BFSTOC;
#pragma pack()

extern BFSTOC bfsTOC;							// BFS Table of Contents
extern unsigned int bfsTOCOffset;				// 4k address of BFSTOC on harddisk. default = 5

int LoadBFSTOC(const char* drive);				//read BFSTOC from drive
