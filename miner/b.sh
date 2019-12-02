#!/bin/sh

CF="-O2 -Wall -fPIC -Wno-pointer-to-int-cast -DCPU_DISPATCH= -fno-asynchronous-unwind-tables"
CC=${PRECC}gcc

$CC $CF bfs.c -c
$CC $CF rbuff.c -c
$CC $CF thread.c -c
if [ "$PRECC" != "" ]; then
$CC $CF -std=gnu99 -mavx512f mshabal_512.c -c
$CC $CF -std=gnu99 -DAVX512F scheduler.c -c
else
$CC $CF -std=gnu99 scheduler.c -c	
fi

$CC $CF -std=gnu99 shabal.c -c
$CC $CF -std=gnu99 sph_shabal.c -c
$CC $CF -mavx -std=gnu99 mshabal_128.c -c
$CC $CF -mavx2 -std=gnu99 mshabal_256.c -c
$CC $CF -msse2 -msse3 -msse4 -msse4.1 -msse4.2 -std=gnu99 mshabal_128_sse2.c -c
# $CC -fPIC -shared -o ../libminer.a *.o -lws2_32

if [ "$PRECC" != "" ]; then
rm -f ../libminer_windows.a
${PRECC}ar rcs ../libminer_windows.a *.o
else
rm -f ../libminer_darwin.a
${PRECC}ar rcs ../libminer_darwin.a *.o	
fi

rm -f *.o
