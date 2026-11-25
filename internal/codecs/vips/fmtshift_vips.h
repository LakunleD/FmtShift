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

/* Colour signalling of a HEIF primary image (fmtshift_heif.c). Values are
 * ITU-T H.273 code points; -1 when there is no NCLX box.
 */
typedef struct {
	int has_icc;
	int primaries;
	int transfer;
} fs_heif_colour;

int fs_init(void);
int fs_has_operation(const char *name);

/* assign_profile, if not NULL, names a libvips built-in ICC profile (e.g.
 * "p3") to attach when the decoded image has none, describing pixels that
 * are already in that colour space.
 */
int fs_convert(int in_fd, int in_fmt, const char *out, int out_fmt,
	int quality, int strip, gint64 max_pixels, const char *assign_profile);
int fs_probe(const char *path, fs_info *info);
int fs_heif_colour_info(int fd, fs_heif_colour *out);

#endif
