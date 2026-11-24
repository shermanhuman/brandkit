# daisyUI release files (third-party, unmodified, MIT)

`daisyui.js` and `daisyui-theme.js` from the GitHub releases https://github.com/saadeghi/daisyui/releases/tag/v5.8.2 and
https://github.com/saadeghi/daisyui/releases/tag/v5.0.35, each with that tag's `LICENSE` (MIT, Pouya Saadeghi). The theme tests
build both of brandkit's theme forms against each version with the Tailwind standalone CLI.

`5.8.2/daisyui.css.gz` is the prebuilt `daisyui.css` from the npm package https://registry.npmjs.org/daisyui/-/daisyui-5.8.2.tgz,
gzipped (`gzip -9n`) and otherwise unmodified; the sum below is of the uncompressed file. The corner test loads it the way a page
without Tailwind does, beside brandkit's `daisyui-themes.css` and `brand.css`.

    54af0da240d110834f96e0a579f99ca4515719a45f4cc0c48266fbbd422d9c1b  5.0.35/daisyui.js
    d917e42ca6bfa54160b13bbb98e79460261b5f1b66bf436a7970a59755a537f4  5.0.35/daisyui-theme.js
    a078297b7d2851dbe8770b8bc20bd7f52b01fc732a29fdf1d0c06514a2771cd8  5.8.2/daisyui.js
    590d2ea77916d5498888b41a37cf1785a46da1e8ea600057aeb971d0d758247e  5.8.2/daisyui-theme.js
    818b8ca8824cf87f43987d844742ac66dadc3a9c07ae2e1f9208afcd9db6d19b  5.8.2/daisyui.css (in daisyui.css.gz)
