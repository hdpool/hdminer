// #include "stdafx.h"
#include <string.h>
#include "mshabal.h"
#include "sph_shabal.h"
#include <x86intrin.h>

// context for 1-dimensional shabal (32bit)
sph_shabal_context global_32;
// context for 4-dimensional shabal (128bit)
mshabal_context global_128;
mshabal_context_fast global_128_fast;
// context for 8-dimensional shabal (256bit)
mshabal256_context global_256;
mshabal256_context_fast global_256_fast;
// context for 16-dimensional shabal (512bit)
mshabal512_context global_512;
mshabal512_context_fast global_512_fast;

//ALL CPUs
void procscoop_sph(hash_arg_t *parg) {
	char const *cache;
	char sig[32 + 128];
	cache = parg->cache;
	char res[32];
	memcpy(sig, parg->signature, sizeof(char) * 32);

	sph_shabal_context x;
	for (unsigned long long v = 0; v < parg->cache_size_local; v++)
	{
		memcpy(&sig[32], &cache[v * 64], sizeof(char) * 64);

		memcpy(&x, &global_32, sizeof(global_32)); // optimization: sph_shabal256_init(&x);
		sph_shabal256(&x, (const unsigned char*)sig, 64 + 32);
		sph_shabal256_close(&x, res);

		unsigned long long *wertung = (unsigned long long*)res;

		if ((*wertung/parg->basetarget) < parg->target_dl) {
			send_report (parg->accid, parg->height, parg->nonce+v, *wertung);
			// ulog ("Nonce","%s accid:%llu, nonce: %llu, height:%llu, target_dl:%llu", 
			// 	parg->iter->Name, parg->accid, parg->nonce+v, parg->height, *wertung/parg->basetarget);
			parg->target_dl = *wertung/parg->basetarget; //收缩
		}
	}
}

