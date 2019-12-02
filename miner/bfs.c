// #include "stdafx.h"
#include "bfs.h"

BFSTOC bfsTOC;							// BFS Table of Contents
unsigned int bfsTOCOffset = 5;			// 4k address of BFSTOC on harddisk. default = 5

#ifdef WIN32
#include <windows.h>
//0, 1
int LoadBFSTOC(const char* drive) {
	//open drive
	HANDLE iFile = CreateFileA(drive, GENERIC_READ, FILE_SHARE_READ, 0, OPEN_EXISTING, FILE_FLAG_NO_BUFFERING, NULL);
	if (iFile == INVALID_HANDLE_VALUE)
	{
		return 0;
	}
	//seek to bfstoc start
	LARGE_INTEGER liDistanceToMove;
	liDistanceToMove.QuadPart = bfsTOCOffset * 4096;
	if (!SetFilePointerEx(iFile, liDistanceToMove, NULL, FILE_BEGIN))
	{
		return 0;
	}
	//read bfstoc sector
	DWORD b;
	if (!ReadFile(iFile, &bfsTOC, (DWORD)(4096), &b, NULL))
	{
		return 0;
	}
	//close drive
	CloseHandle(iFile);
	//check if red content is really a BFSTOC
	char bfsversion[4] = { 'B', 'F', 'S', '1' };

	if (*bfsTOC.version != *bfsversion) {
		return 0;	}
	//success
	return 1;
}
#else //win32
int LoadBFSTOC(const char* drive) {return 0;}
#endif