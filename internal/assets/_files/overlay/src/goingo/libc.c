// The few libc functions LLVM-generated code expects. A go-in-go module
// links no libc: it must not depend on anything in the host process.

#include <stddef.h>

void *memcpy(void *dst, const void *src, size_t n) {
	void *d = dst;
	__asm__ volatile("rep movsb" : "+D"(d), "+S"(src), "+c"(n) : : "memory");
	return dst;
}

void *memmove(void *dst, const void *src, size_t n) {
	unsigned char *d = dst;
	const unsigned char *s = src;
	if (d < s || d >= s + n) {
		return memcpy(dst, src, n);
	} else {
		d += n;
		s += n;
		while (n--) *--d = *--s;
	}
	return dst;
}

void *memset(void *dst, int c, size_t n) {
	void *d = dst;
	__asm__ volatile("rep stosb" : "+D"(d), "+c"(n) : "a"(c) : "memory");
	return dst;
}

int memcmp(const void *a, const void *b, size_t n) {
	const unsigned char *x = a, *y = b;
	for (; n; n--, x++, y++) {
		if (*x != *y) return *x - *y;
	}
	return 0;
}

size_t strlen(const char *s) {
	size_t n = 0;
	while (s[n]) n++;
	return n;
}
