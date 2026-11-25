#include <errno.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#include <libheif/heif.h>

#include "fmtshift_vips.h"

/* A libheif reader over an already-open file descriptor. It uses pread, so
 * it never moves the descriptor's shared file offset (libvips reads the same
 * file through a dup of this descriptor).
 */
typedef struct {
	int fd;
	int64_t pos;
	int64_t size;
} fd_reader;

static int64_t
reader_get_position(void *userdata)
{
	return ((fd_reader *) userdata)->pos;
}

static int
reader_read(void *data, size_t size, void *userdata)
{
	fd_reader *r = userdata;
	char *p = data;

	while (size > 0) {
		ssize_t n = pread(r->fd, p, size, (off_t) r->pos);

		if (n < 0 && errno == EINTR)
			continue;
		if (n <= 0)
			return -1;
		p += n;
		size -= (size_t) n;
		r->pos += n;
	}
	return 0;
}

static int
reader_seek(int64_t position, void *userdata)
{
	fd_reader *r = userdata;

	if (position < 0 || position > r->size)
		return -1;
	r->pos = position;
	return 0;
}

static enum heif_reader_grow_status
reader_wait_for_file_size(int64_t target_size, void *userdata)
{
	return target_size > ((fd_reader *) userdata)->size
		? heif_reader_grow_status_size_beyond_eof
		: heif_reader_grow_status_size_reached;
}

static const struct heif_reader fd_reader_api = {
	.reader_api_version = 1,
	.get_position = reader_get_position,
	.read = reader_read,
	.seek = reader_seek,
	.wait_for_file_size = reader_wait_for_file_size,
};

/* Reads the colour signalling of the primary image of a HEIF file.
 *
 * libvips decodes HEIF pixels but does not expose NCLX colour information
 * (primaries and transfer function), so HDR (PQ/HLG) and wide-gamut NCLX
 * images would otherwise be treated as sRGB. Only container metadata is
 * parsed here; no pixels are decoded. fd must be the same descriptor that
 * fs_convert later decodes, so the policy applies to the pixels it decodes.
 *
 * On failure the libheif message is added to the libvips error buffer.
 */
int
fs_heif_colour_info(int fd, fs_heif_colour *out)
{
	struct heif_context *ctx;
	struct heif_image_handle *handle = NULL;
	struct heif_color_profile_nclx *nclx = NULL;
	struct heif_error err;
	struct stat st;
	fd_reader reader;
	int rc = -1;

	out->has_icc = 0;
	out->primaries = -1;
	out->transfer = -1;

	if (fstat(fd, &st) != 0) {
		vips_error_system(errno, "heif", "cannot stat input");
		return -1;
	}
	reader.fd = fd;
	reader.pos = 0;
	reader.size = (int64_t) st.st_size;

	ctx = heif_context_alloc();
	if (!ctx) {
		vips_error("heif", "out of memory");
		return -1;
	}

	err = heif_context_read_from_reader(ctx, &fd_reader_api, &reader, NULL);
	if (err.code != heif_error_Ok) {
		vips_error("heif", "%s", err.message);
		goto done;
	}
	err = heif_context_get_primary_image_handle(ctx, &handle);
	if (err.code != heif_error_Ok) {
		vips_error("heif", "%s", err.message);
		goto done;
	}

	out->has_icc = heif_image_handle_get_raw_color_profile_size(handle) > 0;

	err = heif_image_handle_get_nclx_color_profile(handle, &nclx);
	if (err.code == heif_error_Ok) {
		out->primaries = nclx->color_primaries;
		out->transfer = nclx->transfer_characteristics;
	}
	else if (err.code != heif_error_Color_profile_does_not_exist) {
		vips_error("heif", "%s", err.message);
		goto done;
	}

	rc = 0;

done:
	if (nclx)
		heif_nclx_color_profile_free(nclx);
	if (handle)
		heif_image_handle_release(handle);
	heif_context_free(ctx);
	return rc;
}
