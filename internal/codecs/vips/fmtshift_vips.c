#include "fmtshift_vips.h"

int
fs_init(void)
{
	if (VIPS_INIT("fmtshift"))
		return -1;

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
load(const char *path, int fmt, VipsImage **out)
{
	switch (fmt) {
	case FS_HEIC:
		/* libheif applies irot/imir transforms and libvips removes the
		 * now-stale orientation tag, so autorot below is a no-op for HEIF.
		 */
		return vips_heifload(path, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	case FS_JPEG:
		return vips_jpegload(path, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	case FS_PNG:
		return vips_pngload(path, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
	case FS_WEBP:
		return vips_webpload(path, out, "fail_on", VIPS_FAIL_ON_ERROR, NULL);
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
fs_convert(const char *in, int in_fmt, const char *out, int out_fmt,
	int quality, int strip, gint64 max_pixels)
{
	/* Every intermediate image is owned by this context and released
	 * together when it is unreffed.
	 */
	VipsObject *context = VIPS_OBJECT(vips_image_new());
	VipsImage **t = (VipsImage **) vips_object_local_array(context, 4);
	VipsImage *image;
	VipsForeignKeep keep;
	int rc;

	if (out_fmt != FS_JPEG && out_fmt != FS_PNG && out_fmt != FS_WEBP) {
		rc = FS_ERR_FORMAT;
		goto done;
	}

	if (load(in, in_fmt, &t[0])) {
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
