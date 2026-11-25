#include <errno.h>
#include <unistd.h>

#include "fmtshift_vips.h"

/* libvips reports some decoder problems (e.g. libpng errors) only as GLib
 * warnings, which GLib prints to stderr. A library must not write to the
 * CLI's stderr, and those warnings are often the only explanation of a
 * failure, so append them to the libvips error buffer instead. They are
 * emitted on the thread doing the failing work, so the codec's error
 * attribution (retry under an exclusive lock) covers them too.
 */
static void
capture_warning(const gchar *domain, GLogLevelFlags level,
	const gchar *message, gpointer user_data)
{
	vips_error("warning", "%s", message);
}

int
fs_init(void)
{
	if (VIPS_INIT("fmtshift"))
		return -1;

	g_log_set_handler("VIPS", G_LOG_LEVEL_WARNING, capture_warning, NULL);

	/* Never fall back to loaders libvips marks as unsafe for untrusted
	 * input (ImageMagick, matio, etc.). We also only ever call explicit
	 * loaders below, so content sniffing cannot pick an unexpected one.
	 */
	vips_block_untrusted_set(TRUE);

	/* One-shot conversions gain nothing from the operation cache and it
	 * would hold decoded images and file handles between jobs.
	 */
	vips_cache_set_max(0);

	return 0;
}

int
fs_has_operation(const char *name)
{
	return vips_type_find("VipsOperation", name) != 0;
}

static int
load(VipsSource *source, int fmt, VipsImage **out)
{
	switch (fmt) {
	case FS_HEIC:
		/* libheif applies irot/imir transforms and libvips removes the
		 * now-stale orientation tag, so autorot below is a no-op for HEIF.
		 */
		return vips_heifload_source(source, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	case FS_JPEG:
		return vips_jpegload_source(source, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	case FS_PNG:
		return vips_pngload_source(source, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	case FS_WEBP:
		return vips_webpload_source(source, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	}
	vips_error("fmtshift", "unsupported input format %d", fmt);
	return -1;
}

static int
save(VipsImage *in, const char *path, int fmt, int quality, VipsForeignKeep keep)
{
	switch (fmt) {
	case FS_JPEG:
		return vips_jpegsave(in, path,
			"Q", quality,
			"keep", keep,
			"optimize_coding", TRUE,
			NULL);
	case FS_PNG:
		return vips_pngsave(in, path, "keep", keep, NULL);
	case FS_WEBP:
		return vips_webpsave(in, path, "Q", quality, "keep", keep, NULL);
	}
	vips_error("fmtshift", "unsupported output format %d", fmt);
	return -1;
}

int
fs_convert(int in_fd, int in_fmt, const char *out, int out_fmt,
	int quality, int strip, gint64 max_pixels, const char *assign_profile)
{
	/* Every intermediate image is owned by this context and released
	 * together when it is unreffed.
	 */
	VipsObject *context = VIPS_OBJECT(vips_image_new());
	VipsImage **t = (VipsImage **) vips_object_local_array(context, 5);
	VipsSource *source = NULL;
	VipsImage *image;
	VipsForeignKeep keep;
	int rc;

	if (out_fmt != FS_JPEG && out_fmt != FS_PNG && out_fmt != FS_WEBP) {
		rc = FS_ERR_FORMAT;
		goto done;
	}

	/* Read the caller's already-open descriptor, never the path, so the
	 * pixels decoded here come from the same file that was inspected. The
	 * source dups the descriptor, sharing its offset, so rewind first in
	 * case this is a retry.
	 */
	if (lseek(in_fd, 0, SEEK_SET) != 0) {
		vips_error_system(errno, "fmtshift", "cannot rewind input");
		rc = FS_ERR_LOAD;
		goto done;
	}
	if (!(source = vips_source_new_from_descriptor(in_fd))) {
		rc = FS_ERR_LOAD;
		goto done;
	}

	if (load(source, in_fmt, &t[0])) {
		rc = FS_ERR_LOAD;
		goto done;
	}
	image = t[0];

	/* For HEIF this counts top-level images only; thumbnails, depth maps
	 * and gain maps are auxiliary and not included.
	 */
	if (vips_image_get_n_pages(image) > 1) {
		rc = FS_ERR_MULTI;
		goto done;
	}

	if ((gint64) image->Xsize * image->Ysize > max_pixels) {
		rc = FS_ERR_TOO_LARGE;
		goto done;
	}

	rc = FS_ERR_PROCESS;

	/* Apply EXIF orientation to the pixels and remove the tag, so
	 * preserved metadata cannot make a viewer rotate the image again.
	 */
	if (vips_autorot(image, &t[1], NULL))
		goto done;
	image = t[1];

	/* Pixels in a colour space signalled only by NCLX (e.g. Display P3
	 * without an ICC profile) get the matching ICC profile, so both the
	 * kept-metadata and sRGB-conversion paths below interpret them
	 * correctly. The pixels themselves are not changed.
	 */
	if (assign_profile && !vips_image_get_typeof(image, VIPS_META_ICC_NAME)) {
		VipsBlob *profile;
		const void *data;
		size_t length;

		if (vips_profile_load(assign_profile, &profile, NULL))
			goto done;
		if (vips_copy(image, &t[4], NULL)) {
			vips_area_unref(VIPS_AREA(profile));
			goto done;
		}
		data = vips_blob_get(profile, &length);
		vips_image_set_blob_copy(t[4], VIPS_META_ICC_NAME, data, length);
		vips_area_unref(VIPS_AREA(profile));
		image = t[4];
	}

	/* Without metadata the ICC profile is dropped too, so bake colours
	 * into sRGB first rather than letting them be misinterpreted.
	 */
	if (strip && vips_image_get_typeof(image, VIPS_META_ICC_NAME)) {
		if (vips_icc_transform(image, &t[2], "srgb",
				"embedded", TRUE,
				"intent", VIPS_INTENT_RELATIVE,
				NULL))
			goto done;
		image = t[2];
	}

	/* JPEG has no alpha: composite onto white instead of the savers'
	 * default black.
	 */
	if (out_fmt == FS_JPEG && vips_image_hasalpha(image)) {
		double white = vips_interpretation_max_alpha(image->Type);
		VipsArrayDouble *background = vips_array_double_newv(1, white);
		int failed = vips_flatten(image, &t[3],
			"background", background,
			NULL);

		vips_area_unref(VIPS_AREA(background));
		if (failed)
			goto done;
		image = t[3];
	}

	/* Gain maps are excluded: v0.1 writes the SDR base image only. */
	keep = strip ? VIPS_FOREIGN_KEEP_NONE
				 : (VIPS_FOREIGN_KEEP_EXIF | VIPS_FOREIGN_KEEP_XMP |
					   VIPS_FOREIGN_KEEP_IPTC | VIPS_FOREIGN_KEEP_ICC |
					   VIPS_FOREIGN_KEEP_OTHER);

	if (save(image, out, out_fmt, quality, keep))
		goto done;

	rc = FS_OK;

done:
	g_object_unref(context);
	if (source)
		g_object_unref(source);
	return rc;
}

int
fs_probe(const char *path, fs_info *info)
{
	VipsImage *image = vips_image_new_from_file(path, NULL);

	if (!image)
		return -1;

	info->width = image->Xsize;
	info->height = image->Ysize;
	info->pages = vips_image_get_n_pages(image);
	info->orientation = vips_image_get_orientation(image);
	info->bands = image->Bands;
	info->has_alpha = vips_image_hasalpha(image);
	info->has_icc = vips_image_get_typeof(image, VIPS_META_ICC_NAME) != 0;
	info->has_exif = vips_image_get_typeof(image, VIPS_META_EXIF_NAME) != 0;

	g_object_unref(image);
	return 0;
}
