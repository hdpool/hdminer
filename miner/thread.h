//thread.h
#ifndef _THREAD_H_
#define _THREAD_H_

#ifdef __cplusplus
extern "C"{
#endif

typedef struct thread_t thread_t;
typedef void (*thread_func)(thread_t *thd);

thread_t *thread_create(thread_func func, void *arg);
void thread_destroy(thread_t *me);

void thread_stop(thread_t *me);
int thread_testcancel(thread_t *me);
void* thread_getarg(thread_t *me);

#ifdef __cplusplus
}
#endif

#endif //_THREAD_H_

