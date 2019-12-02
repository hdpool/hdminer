//thread.c
#include <assert.h>
#include <stdio.h>
#include <time.h>
#include <errno.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>
#include "thread.h"

#ifdef WIN32
	#include <windows.h>
	#include <process.h>
	#define pthread_t HANDLE
#else
	#include <pthread.h>
#endif

struct thread_t {
	pthread_t pid;
	thread_func func;
	void *arg;
	volatile int runflag;
#ifdef WIN32
	HANDLE runevent;
#endif
};

#ifdef WIN32
static unsigned __stdcall thread_run(void *arg)
#else
static void* thread_run(void *arg)
#endif
{
	thread_t *me = (thread_t *)arg;
#ifdef WIN32
	SetEvent(me->runevent);//通知线程已启动
#else
	//子线程不接任何信号
	sigset_t mask;
	sigfillset(&mask);
	//sigdelset (&mask, 0); //test alive
	pthread_sigmask(SIG_BLOCK, &mask, NULL);
#endif

	//me->runflag=1;//BUG : 当create后马上stop时，还没到这
	me->func(me);
    me->runflag=0;
    
#ifdef WIN32
	SetEvent(me->runevent);//通知线程将退出
	_endthread();
#else
	//pthread_exit(NULL);//在join的情况下，调用这个反而memory leak
#endif
	return 0;
}

thread_t *thread_create(thread_func func, void *arg)
{
	int ret=0;
#ifdef WIN32
	unsigned threadid=0;
#else
	pthread_attr_t attr;
#endif
	thread_t *me=(thread_t*)calloc(1, sizeof(*me));
	if (me==NULL)
		return NULL;
	me->func = func;
	me->arg=arg;

	me->runflag=1;
#ifdef WIN32
	me->runevent = CreateEvent(NULL, FALSE, FALSE, NULL);
	if (me->runevent==0) {
		goto Failed;
	}
	me->pid = (HANDLE)_beginthreadex(NULL, 0, &thread_run, me, 0, &threadid);
	if (me->pid == 0) {
		goto Failed;
	}
	
	//等待线程启动
	ret = (int)WaitForSingleObject(me->runevent, INFINITE);
	assert (ret == (int)WAIT_OBJECT_0);	
#else
	pthread_attr_init (&attr);
	pthread_attr_setscope( &attr, PTHREAD_SCOPE_SYSTEM );
	pthread_attr_setdetachstate( &attr, PTHREAD_CREATE_JOINABLE );
	ret = pthread_create(&me->pid, &attr, thread_run, me);
	if (ret!=0) {
		me->pid=0;
		goto Failed;
	}
#endif	

	return me;
Failed:
    thread_stop(me);
	thread_destroy(me);
	return NULL;
}

void thread_destroy(thread_t *me)
{
	if (me) {
#ifdef WIN32
		if (me->runevent) {
			if (me->pid) {
				WaitForSingleObject(me->pid, INFINITE);
				CloseHandle(me->pid);
			}
			CloseHandle(me->runevent);
		}
#else
		if (me->pid) {
			pthread_join(me->pid, NULL);
		}
#endif
		free(me);
	}
}

void thread_stop(thread_t *me)
{
	me->runflag=0;
}

int thread_testcancel(thread_t *me)
{
	return me->runflag==0;
}

void* thread_getarg(thread_t *me)
{
	return me->arg;
}

