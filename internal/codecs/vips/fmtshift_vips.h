#ifndef FMTSHIFT_VIPS_H
#define FMTSHIFT_VIPS_H

#include <vips/vips.h>

/* Formats, mirrored in vips.go. */
enum { FS_HEIC = 1, FS_JPEG, FS_PNG, FS_WEBP };

/* Result codes for fs_convert. */
enum {
	FS_OK = 0,
	FS_ERR_LOAD = 1,      /* input could not be opened */
	FS_ERR_MULTI = 2,     /* more than one top-level image / frame */
	FS_ERR_TOO_LARGE = 3, /* width * height exceeds max_pixels */
	FS_ERR_PROCESS = 4,   /* pixel processing or saving failed */
	FS_ERR_FORMAT = 5     /* unsupported format code */
};

typedef struct {
	int width;
	int height;
	int pages;
	int orientation;
	int bands;
	int has_alpha;
	int has_icc;
	int has_exif;
} fs_info;

int fs_init(void);
int fs_has_operation(const char *name);
int fs_convert(const char *in, int in_fmt, const char *out, int out_fmt,
	int quality, int strip, gint64 max_pixels);
int fs_probe(const char *path, fs_info *info);

#endif
