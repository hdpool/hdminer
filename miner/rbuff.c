//rbuff.c
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
// #include <malloc.h>
#include <assert.h>
#include <stdarg.h>
#include <ctype.h> //toupper
#include "rbuff.h"

#ifdef _MSC_VER
#pragma warning( disable : 4018 )
#define alloca _alloca
#define vsnprintf _vsnprintf
#else
// #include <alloca.h>
#endif

rbuff_t *rbuff_init (char *mem, unsigned int size)
{
    rbuff_t *buff = (rbuff_t *)mem;

    if (!buff) {
        assert (size > 0);
        buff = (rbuff_t *)malloc (sizeof(rbuff_t) + size + 1);

        if (!buff) {
            return NULL;
        }
    } else {
        //当使用已有内存时，要保留出空间
        assert (size > sizeof(rbuff_t) + 1);
        size -= sizeof(rbuff_t) + 1;
    }

    memset (buff, 0, sizeof(rbuff_t));
    buff->hp = (char *)(buff + 1);
    //buff->count = size+1;

    if (!mem) {
        buff->selfalloc = 1;
    }

    buff->wp = buff->rp = buff->hp;
    buff->tp = buff->hp + size + 1;

    return buff;
}

void rbuff_cleanup(rbuff_t *buff)
{
    if (buff) {
        if (buff->selfalloc) {
            free (buff);
        }
    }
}

void rbuff_reset(rbuff_t *buff)
{
    buff->wp = buff->rp = buff->hp;
}

unsigned rbuff_put(rbuff_t *buff, void *data, unsigned size)
{
    char *buf = (char *)data;
    char *readptr   = buff->rp;
    unsigned part       = buff->tp - buff->wp;

    if (buff->wp >= readptr) {
        if (size > (buff->tp - buff->hp - (buff->wp - readptr) - 1)) {
            size = buff->tp - buff->hp - (buff->wp - readptr) - 1;
        }

        if (part >= size) {
            if (buf) {
                memcpy(buff->wp, buf, size);
            }

            buff->wp += size;
        } else { //part < size
            if (buf) {
                memcpy(buff->wp, buf, part);
                buf += part;
            }

            size -= part;

            if (size > (buff->rp - buff->hp - 1)) {
                size = buff->rp - buff->hp - 1;
            }

            if (buf) {
                memcpy(buff->hp, buf, size);
            }

            buff->wp = buff->hp + size;
            size += part;
        }
    } else { // wptr_ < readptr
        if (size > (readptr - buff->wp - 1)) {
            size = readptr - buff->wp - 1;
        }

        if (buf) {
            memcpy(buff->wp, buf, size);
        }

        buff->wp += size;
    }

    return size;
}

unsigned _rbuff_get(rbuff_t *buff, void *data, unsigned size, int peek_flag)
{
    char *buf = (char *)data;
    char *writeptr  = buff->wp;
    unsigned part   = buff->tp - buff->rp;

    if (writeptr >= buff->rp) {
        if (size > (writeptr - buff->rp)) {
            size = writeptr - buff->rp;
        }

        if (buf) {
            memcpy(buf, buff->rp, size);
        }

        if (!peek_flag) {
            buff->rp += size;
        }
    } else { // writeptr < rptr_
        if (size > ((buff->tp - buff->hp) - (buff->rp - writeptr))) {
            size = (buff->tp - buff->hp) - (buff->rp - writeptr);
        }

        if (part >= size) {
            if (buf) {
                memcpy(buf, buff->rp, size);
            }

            if (!peek_flag) {
                buff->rp += size;
            }
        } else { // part < size
            if (buf) {
                memcpy(buf, buff->rp, part);
                buf += part;
            }

            size -= part;

            if (size > (writeptr - buff->hp)) {
                size = writeptr - buff->hp;
            }

            if (buf) {
                memcpy(buf, buff->hp, size);
            }

            if (!peek_flag) {
                buff->rp = buff->hp + size;
            }

            size += part;
        }
    }

    return size;
}

char *rbuff_get_write_ptrs(rbuff_t *buff, char **p1, unsigned int *len1, char **p2, unsigned int *len2)
{
    char *writeptr  = buff->wp;
    char *readptr = buff->rp;

    unsigned int alen1, alen2;
    char *ap1, *ap2;

    if (!len1) {
        len1 = &alen1;
    }

    if (!len2) {
        len2 = &alen2;
    }

    if (!p1) {
        p1 = &ap1;
    }

    if (!p2) {
        p2 = &ap2;
    }

    if(writeptr == readptr) { //no data
        if(writeptr == buff->hp || writeptr == buff->tp) {
            *p1 = buff->hp;
            *len1 = buff->tp - buff->hp  - 1;
            *p2 = 0;
            *len2 = 0;
        } else {
            *p1 = writeptr;
            *len1 = buff->tp - writeptr;
            *p2 = buff->hp;
            *len2 = readptr - buff->hp - 1;

            if(*len2 == 0) {
                *p2 = 0;
            }
        }

        assert(*len1 + *len2 == buff->tp - buff->hp - 1);
    } else if(writeptr > readptr) {
        *p1 = writeptr;
        *len1 = buff->tp - writeptr;

        if(readptr == buff->hp) {
            (*len1) --;
        }

        if(*(int *)len1 < 0) {
            *len1 = 0;
        }

        if(*len1 == 0) {
            *p1 = 0;
        }

        *p2 = buff->hp;
        *len2 = readptr - buff->hp - 1;

        if(*(int *)len2 < 0) {
            *len2 = 0;
        }

        if(*len2 == 0) {
            *p2 = 0;
        }

        if (*p2 && !*p1) {//exchange p1 <--> p2
            *p1 = *p2;
            *len1 = *len2;
            *p2 = 0;
            *len2 = 0;
        }

        assert(*len1 + *len2 == (buff->tp - buff->hp) - 1 - (writeptr - readptr));
    } else {
        *p1 = writeptr;
        *len1 = readptr - writeptr - 1;

        if(*len1 == 0) {
            *p1 = 0;
        }

        *p2 = 0;
        *len2 = 0;
        assert(*len1 + *len2 == readptr - writeptr - 1);
    }

    return *p1;
}

char *rbuff_get_read_ptrs(rbuff_t *buff, char **p1, unsigned int *len1, char **p2, unsigned int *len2)
{
    char *writeptr  = buff->wp;
    char *readptr = buff->rp;

    unsigned int alen1, alen2;
    char *ap1, *ap2;

    if (!len1) {
        len1 = &alen1;
    }

    if (!len2) {
        len2 = &alen2;
    }

    if (!p1) {
        p1 = &ap1;
    }

    if (!p2) {
        p2 = &ap2;
    }

    if(writeptr == readptr) {
        *p1 = 0;
        *len1 = 0;
        *p2 = 0;
        *len2 = 0;
    } else if(writeptr > readptr) {
        *p2 = 0;
        *len2 = 0;
        *len1 = writeptr - readptr;
        *p1 = readptr;
    } else {
        *len1 = buff->tp - readptr;
        *p1 = readptr;

        if(*len1 == 0) {
            *p1 = 0;
        }

        *p2 = buff->hp;
        *len2 = writeptr - buff->hp;

        if(*len2 == 0) {
            *p2 = 0;
        }

        if (*p2 && !*p1) {//exchange p1 <--> p2
            *p1 = *p2;
            *len1 = *len2;
            *p2 = 0;
            *len2 = 0;
        }
    }

    return *p1;
}

int rbuff_ptr_pos (rbuff_t *buff, const char *ptr)
{
    // unsigned int i;
    unsigned int len1, len2;
    char *p1, *p2;
    // const char *pstr = ptr;
    assert (ptr && ptr[0] != '\0');

    if (!ptr || ptr[0] == '\0') {
        return -1;
    }
    rbuff_get_read_ptrs (buff, &p1, &len1, &p2, &len2);
    if (!p1) {
        return -1;
    }
    if (ptr>p1 && ptr<p1+len1) {
        return ptr-p1;
    }
    if (!p2) {
        return -1;
    }
    if (ptr>p2 && ptr<p2+len2) {
        return ptr-p2 + len1;
    }
    return -1;
}
static inline int is_equ (unsigned char c1, unsigned char c2, int ignore_case)
{
    if (!ignore_case) {
        return c1 == c2;
    }

    return toupper(c1) == toupper(c2);
}
int rbuff_string_pos(rbuff_t *buff, const char *str, int ignore_case)
{
    unsigned int i;
    unsigned int len1, len2;
    char *p1, *p2;
    const char *pstr = str;

    if (!str || str[0] == '\0') {
        assert (0);
        return -1;
    }

    rbuff_get_read_ptrs(buff, &p1, &len1, &p2, &len2);

    if (p1) {
        for (i = 0; *pstr && i < len1; i++) {
            if (p1[i] == '\0') {
                return -1;
            }

            // if (p1[i] == *pstr) {
            if (is_equ(p1[i], *pstr, ignore_case)) {
                pstr++;
            } else { // if (pstr!=str)
                i -= (pstr - str);
                pstr = str;
            }
        }

        if (*pstr == '\0') {
            return i - (pstr - str);
        }
    }

    if (p2) {
        for (i = 0; *pstr && i < len2; i++) {
            if (p2[i] == '\0') {
                return -1;
            }

            if (p2[i] == *pstr) {
                pstr++;
            } else { // if (pstr!=str)
                i -= (pstr - str);
                pstr = str;
            }
        }

        if (*pstr == '\0') {
            return len1 + i - (pstr - str);
        }
    }

    return -1;
}

int rbuff_string_pbrk(rbuff_t *buff, const char *str)
{
    unsigned int i;
    unsigned int len1, len2;
    char *p1, *p2, *ptmp;
    assert (str && str[0] != '\0');

    if (!str || str[0] == '\0') {
        return -1;
    }

    rbuff_get_read_ptrs(buff, &p1, &len1, &p2, &len2);

    if (p1) {
        for (i = 0; i < len1; i++) {
            if (p1[i] == '\0') {
                return -1;
            }

            //reverse check
            ptmp = strchr (str, p1[i]);

            if (ptmp) {
                return i;
            }
        }
    }

    if (p2) {
        for (i = 0; i < len2; i++) {
            if (p2[i] == '\0') {
                return -1;
            }

            ptmp = strchr (str, p2[i]);

            if (ptmp) {
                return len1 + i;
            }
        }
    }

    return -1;
}

int rbuff_mem_pos(rbuff_t *buff, void *data, unsigned size)
{
    unsigned int i;
    unsigned int len1, len2;
    char *p1, *p2, *pstr = (char *)data;
    assert (data && size > 0);

    if (!data || size == 0) {
        return -1;
    }

    rbuff_get_read_ptrs(buff, &p1, &len1, &p2, &len2);

    if (p1) {
        for (i = 0; pstr - size != (char *)data && i < len1; i++) {
            if (p1[i] == *pstr) {
                pstr++;
            } else { // if (pstr!=str)
                i -= (pstr - (char *)data);
                pstr = (char *)data;
            }
        }

        if (pstr - size == (char *)data) {
            return i - (pstr - (char *)data);
        }
    }

    if (p2) {
        for (i = 0; pstr - size != (char *)data && i < len2; i++) {
            if (p2[i] == *pstr) {
                pstr++;
            } else { // if (pstr!=str)
                i -= (pstr - (char *)data);
                pstr = (char *)data;
            }
        }

        if (pstr - size == (char *)data) {
            return len1 + i - (pstr - (char *)data);
        }
    }

    return -1;
}

unsigned rbuff_peek_after_pos (rbuff_t *buff, void *data, unsigned size, unsigned pos)
{
    rbuff_t buff_clone;

    if (pos >= rbuff_datacount(buff)) {
        return 0;
    }

    memcpy (&buff_clone, buff, sizeof(rbuff_t));
    rbuff_erase (&buff_clone, pos);
    size = rbuff_peek (&buff_clone, data, size);

    return size;
}
unsigned rbuff_move(rbuff_t *dest, rbuff_t *src, unsigned size)
{
    unsigned len = 0;
    unsigned int len1, len2;
    char *p1, *p2;
    rbuff_get_read_ptrs(src, &p1, &len1, &p2, &len2);

    if (p1 && size > 0 && rbuff_freecount(dest) > 0) {
        if (len1 > size) {
            len1 = size;
        }

        len += rbuff_put (dest, p1, len1);
        size -= len;
    }

    if (p2 && size > 0 && rbuff_freecount(dest) > 0) {
        if (len2 > size) {
            len2 = size;
        }

        len += rbuff_put (dest, p2, len2);
        size -= len;
    }

    if (len > 0) {
        rbuff_erase (src, len);
    }

    return len;
}

unsigned rbuff_add_print(rbuff_t *buff, const char *fmt, ...)
{
    int ret;
    unsigned blen = rbuff_freecount (buff);
    char *data = (char *)alloca (blen + 1);
    va_list ap;

    if (blen == 0) {
        return 0;
    }

    va_start (ap, fmt);
    ret = vsnprintf (data, blen + 1, fmt, ap);
    va_end (ap);

    if ((unsigned)ret > blen) { //C99
        ret = blen;
    }

    return rbuff_put (buff, data, (unsigned)ret);
}

////////////////////////////////////////////////////////////////////
// taskq 任务队列，单生产单消费模型时，消息为定长的

int taskq_init (taskq_t *me, unsigned maxtasks, unsigned tasklen)
{
	assert (maxtasks > 0 && tasklen > 0);
	me->maxtasks = maxtasks;
	me->tasklen = tasklen;
	
	me->rb = rbuff_init (NULL, maxtasks*tasklen);
	
	return me->rb ? 0 : -1;
}

//数据复制
int taskq_push (taskq_t *me, void *task, unsigned tlen)
{
	//unsigned ret;
	assert (tlen > 0);
	
	if (rbuff_freecount (me->rb) < me->tasklen) {
		return -1;
	}
	
	if (tlen > me->tasklen) {
		tlen = me->tasklen;
	}
	
	/*ret = */rbuff_put (me->rb, task, tlen);
	if (tlen < me->tasklen) {
		/*ret = */rbuff_step_write_ptr (me->rb, me->tasklen-tlen);
	}
	
	return 0;
}

//要求task有效内存长为tasklen
int taskq_pop (taskq_t *me, void *task)
{
	unsigned ret;

	ret = rbuff_get (me->rb, task, me->tasklen);
	if (ret > 0) {
		assert (ret == me->tasklen);
		return 0;
	}
	return -1;
}

///////////////////////////////////////////////////////////////////
// bqueue 单生产，单消费模型，用于大块rbuff内存传递

int bqueue_init (bqueue_t *bq, int max_idles) 
{
	memset (bq, 0, sizeof(*bq));
	
	bq->max_bufs = max_idles;

	bq->free_queue = rbuff_init (NULL, max_idles * sizeof(void*));
	bq->busy_queue = rbuff_init (NULL, max_idles * sizeof(void*));
	
	return 0;
}

void bqueue_cleanup (bqueue_t *bq) 
{
	rbuff_t *b = NULL;
	
	if (!bq->free_queue) return;
	
	while (rbuff_get (bq->free_queue, &b, sizeof(void*)) != 0) {
		rbuff_cleanup (b);
	}
	rbuff_cleanup (bq->free_queue);
	bq->free_queue = NULL;

	while (rbuff_get (bq->busy_queue, &b, sizeof(void*)) != 0) {
		rbuff_cleanup (b);
	}
	rbuff_cleanup (bq->busy_queue);
	bq->busy_queue = NULL;
}

rbuff_t* bqueue_producer_get(bqueue_t *bq, size_t blen)
{
	rbuff_t *b = NULL;
	
	if (rbuff_get (bq->free_queue, &b, sizeof(void*)) == 0
		&& bq->alloced_bufs < bq->max_bufs)
	{
		b = rbuff_init (NULL, blen);
		if (b) {
			bq->alloced_bufs++;
		}
	}
	
	if (b && rbuff_maxcount(b) < blen) {
		rbuff_cleanup (b);
		b = rbuff_init (NULL, blen);
	}

	return b;
}

void bqueue_producer_free (bqueue_t *bq, rbuff_t *b)
{
	b->needfree = 1;//(void*)&rbuff_cleanup;
	bqueue_producer_put (bq, b);
}

void bqueue_consumer_free (bqueue_t *bq, rbuff_t *b)
{
	rbuff_reset (b);
	b->needfree = 0;

	rbuff_put (bq->free_queue, &b, sizeof(void*));
}

rbuff_t* bqueue_consumer_get (bqueue_t *bq)
{
	rbuff_t *b=NULL;

	do {
		b = NULL;
		rbuff_get (bq->busy_queue, &b, sizeof(void*));
		if (!b || !b->needfree) {
			break;
		}
		bqueue_consumer_free (bq, b);
	} while (1);
	
	return b;
}

///////////////////////////////////////////////////////////////////
// pbuff 简单缓冲

void pbuff_init (pbuff_t *p) 
{
    p->ptr = NULL;
    p->size = p->used = 0;
}

void pbuff_cleanup (pbuff_t *p)
{
    if (p->ptr) free(p->ptr);
    pbuff_init(p);
}

char* pbuff_alloc(pbuff_t *p, unsigned len)
{
    p->ptr = (char*)malloc(len);
    if (p->ptr) {
        p->size = len;
        p->used = 0;
    }
    return p->ptr;
}

char* pbuff_realloc(pbuff_t *p, unsigned len)
{
    if (p->size >= len) return p->ptr;
    if (p->ptr) free (p->ptr);
    return pbuff_alloc(p, len);
}

void pbuff_push(pbuff_t *p, void *data, unsigned len)
{
    if (p->used+len <= p->size) {
        memcpy(p->ptr+p->used, data, len);
        p->used += len;
    }
}
