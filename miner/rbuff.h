//rbuff.h : ring buffer
#ifndef _RBUFF_H_
#define _RBUFF_H_

#include <stdlib.h> //free
#include <string.h> //memcpy
#include <assert.h> //assert

#ifdef _MSC_VER
	#undef inline
	#define inline _inline
#endif

#ifdef __cplusplus
extern "C" {
#endif

//sizeof == 6 * 4 == 24B
typedef struct {
	char 	*rp; //读指针
	char 	*wp; //写指针
	char 	*hp; //缓冲区的头指针
	char 	*tp; //缓冲区的尾指针
	unsigned selfalloc:1;//是否是由buff创建的内存
	unsigned needfree:1;
} rbuff_t;

//当使用已有内存时，size要保留出空间(sizeof(rbuff_t) + 1)
rbuff_t*	rbuff_init (char *mem, unsigned int size);
//释放内存
void        rbuff_cleanup(rbuff_t*);
//复位读写指针
void        rbuff_reset(rbuff_t*);

#define 	rbuff_maxcount(b) ((b)->tp - (b)->hp -1)
#define 	rbuff_freecount(b) ((b)->wp>=(b)->rp ? ((b)->tp - (b)->hp) -((b)->wp-(b)->rp)-1 : (b)->rp-(b)->wp-1)
#define 	rbuff_datacount(b) ((b)->wp>=(b)->rp ? (b)->wp-(b)->rp : ((b)->tp - (b)->hp) -((b)->rp-(b)->wp))

unsigned	rbuff_put(rbuff_t*, void* data, unsigned size);
unsigned	_rbuff_get(rbuff_t*, void* data, unsigned size, int peek_flag);
#define 	rbuff_get(buff, pdata, size) _rbuff_get(buff, pdata, size, 0)
#define 	rbuff_peek(buff, pdata, size) _rbuff_get(buff, pdata, size, 1)
#define		rbuff_erase(buff, size) _rbuff_get(buff, NULL, size, 0)
// void 		rbuff_reset_head (rbuff_t *buff);

#define 	rbuff_get_read_ptr(buff, plen) rbuff_get_read_ptrs(buff, NULL, plen, NULL, NULL)
#define 	rbuff_get_write_ptr(buff, plen) rbuff_get_write_ptrs(buff, NULL, plen, NULL, NULL)

char* 		rbuff_get_read_ptrs(rbuff_t* buff, char **p1, unsigned int *len1, char **p2, unsigned int *len2);
char* 		rbuff_get_write_ptrs(rbuff_t* buff, char** p1, unsigned int* len1, char **p2, unsigned int *len2);
#define 	rbuff_step_write_ptr(buff, size) rbuff_put(buff, NULL, size)

int 		rbuff_ptr_pos (rbuff_t *buff, const char *ptr);
int 		rbuff_string_pos(rbuff_t *buff, const char *str, int ignore_case);
int 		rbuff_string_pbrk(rbuff_t *buff, const char *str);
int 		rbuff_mem_pos(rbuff_t *buff, void *data, unsigned size);

unsigned    rbuff_peek_after_pos (rbuff_t *buff, void *data, unsigned size, unsigned pos);

//just copy data
unsigned 	rbuff_move (rbuff_t *dest, rbuff_t *src, unsigned size);

//增加进rbuff，返回这次增加的字节数。很低效率的函数
unsigned 	rbuff_add_print(rbuff_t *buff, const char *fmt, ...);

//想要做ref提供多份共享? 可以自己管理内存，再rbuff_init(buf, len);

typedef struct {
        unsigned cap;
        unsigned roff;
        unsigned woff;
        char buf[0];
} tbuff_t;

#define tbuff_datacount(t) ((t)->woff-(t)->roff)
#define tbuff_freecount(t) ((t)->cap-(t)->woff)
#define tbuff_maxcount(t) ((t)->cap)

static inline tbuff_t *tbuff_init (void *b, unsigned len)
{
	tbuff_t *t;
	if (b) t = (tbuff_t*)b;
	else t = (tbuff_t*)malloc (len);
	t->cap = len - sizeof(tbuff_t);
	t->roff = t->woff = 0;
	return t;
}

// #ifdef _MSC_VER
#define __min_(x,y) ((x)>(y) ? (y) : (x))
// #endif

static inline unsigned tbuff_peek (tbuff_t *t, void *b, unsigned len)
{
	len = __min_ (len, tbuff_datacount(t));
	memmove (b, t->buf+t->roff, len);
	return len;
}
static inline unsigned tbuff_get (tbuff_t *t, void *b, unsigned len)
{
	len = __min_ (len, tbuff_datacount(t));
	memmove (b, t->buf+t->roff, len);
	t->roff += len;
	return len;
}
static inline char *tbuff_get_rptr (tbuff_t *t, unsigned *plen)
{
	if (plen) *plen = tbuff_datacount (t);
	return t->buf + t->roff;
}
static inline char *tbuff_get_wptr (tbuff_t *t, unsigned *plen)
{
	if (plen) *plen = tbuff_freecount (t);
	return t->buf + t->woff;
}

#define tbuff_erase(t, len) (t)->roff += len
#define tbuff_step_wptr(t, len) (t)->woff += len

#define tbuff_reset_head(t) do { \
        if ((t)->roff > 0) { \
                memmove((t)->buf, (t)->buf+(t)->roff, (t)->woff-(t)->roff); \
                (t)->woff -= (t)->roff; \
                (t)->roff = 0; \
        } \
} while(0)

////////////////////////////////////////////////////////////////////
// taskq 任务队列，单生产单消费模型时，消息为定长的
typedef struct {
	rbuff_t *rb;
	unsigned maxtasks;
	unsigned tasklen;
} taskq_t;

int 		taskq_init (taskq_t *me, unsigned maxtasks, unsigned tasklen);
#define 	taskq_cleanup(me) rbuff_cleanup((me)->rb)

int 		taskq_push (taskq_t *me, void *task, unsigned tlen); //数据复制
int 		taskq_pop (taskq_t *me, void *task); //要求task有效内存长为tasklen

///////////////////////////////////////////////////////////////////
// bqueue 单生产，单消费模型，用于大块rbuff内存传递
typedef struct {
	int 	 max_bufs;
	int 	 alloced_bufs;
	rbuff_t *free_queue; //(sizeof (void*) * MAX_IDLE)
	rbuff_t *busy_queue; //(sizeof (void*) * MAX_IDLE)
} bqueue_t;

int 		bqueue_init (bqueue_t *bq, int max_idles) ;
void 		bqueue_cleanup (bqueue_t *bq);

rbuff_t* 	bqueue_producer_get(bqueue_t *bq, size_t blen);
#define 	bqueue_producer_put(bq, b) rbuff_put ((bq)->busy_queue, &b, sizeof(void*))
void 		bqueue_producer_free (bqueue_t *bq, rbuff_t *b);

void 		bqueue_consumer_free (bqueue_t *bq, rbuff_t *b);
rbuff_t* 	bqueue_consumer_get (bqueue_t *bq);

///////////////////////////////////////////////////////////////////
// pbuff 简单缓冲
typedef struct {
    char 	*ptr;
    unsigned size;
    unsigned used;
}pbuff_t;

void 		pbuff_init (pbuff_t *p);
void 		pbuff_cleanup (pbuff_t *p);

char*		pbuff_alloc(pbuff_t *p, unsigned len);
char*		pbuff_realloc(pbuff_t *p, unsigned len);
void 		pbuff_push(pbuff_t *p, void *data, unsigned len);

#ifdef __cplusplus
}
#endif 

#endif //_RBUFF_H_
