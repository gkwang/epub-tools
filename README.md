# epub-tools

A command-line tool for organizing an epub library. It renames each epub file
to the book's title, read from the epub's own metadata — and can optionally
check that title against online catalogs.

```
$ epub-tools rename ~/Books
~/Books/1(60).epub -> 無法成為主角的我們始於妥協的戀人生活 第一卷.epub
~/Books/ngjm2e.epub -> Whistler.epub
would rename 2, unchanged 0, failed 0
(preview only; use --apply to rename)
```

## Install

**macOS and Linux** (installs to `~/.local/bin`, no sudo):

```sh
curl -fsSL https://raw.githubusercontent.com/gkwang/epub-tools/main/install.sh | sh
```

Run the same command again to upgrade. Options, set as environment variables
before `sh`:

- `EPUB_TOOLS_VERSION=v0.1.0` — install a specific release instead of the latest
- `EPUB_TOOLS_INSTALL_DIR=/usr/local/bin` — install somewhere else

The script verifies the download against the release's `checksums.txt` and
stops if it does not match.

**With Go** (1.26.10 or newer):

```sh
go install github.com/gkwang/epub-tools/cmd/epub-tools@latest
```

**Manual download:** pick the archive for your system from the
[releases page](https://github.com/gkwang/epub-tools/releases), extract it,
and put `epub-tools` somewhere on your `PATH`. Windows archives are `.zip`.

> **macOS note:** the binaries are not signed by Apple. If you download an
> archive with a web browser, macOS will refuse to open the binary
> ("cannot be opened because the developer cannot be verified"). Clear the
> download flag once:
>
> ```sh
> xattr -d com.apple.quarantine ./epub-tools
> ```
>
> The `curl | sh` installer and `go install` are not affected.

**Verify a download** (optional, needs the [GitHub CLI](https://cli.github.com)):

```sh
gh attestation verify epub-tools_0.1.0_darwin_arm64.tar.gz --repo gkwang/epub-tools
```

**Uninstall:** `rm ~/.local/bin/epub-tools`

## Usage

```sh
epub-tools rename <dir>                 # preview: show what would be renamed
epub-tools rename --apply <dir>         # rename the files
epub-tools rename --recursive <dir>     # include subfolders
epub-tools rename --lookup <dir>        # check titles against online catalogs
epub-tools rename --chinese simplified <dir>
epub-tools version
```

Nothing changes unless you pass `--apply`. Always preview first.

- **Safe by design.** Existing files are never overwritten; two books with the
  same title become `Title.epub` and `Title (2).epub`. Running it again on an
  organized folder changes nothing. Files without a usable title are reported
  and skipped.
- **Incomplete metadata.** If the epub's main title is missing or junk
  ("Unknown", "Untitled"), the title from the table of contents is used
  instead. The language is checked against the script the title is written
  in, so a Chinese book mislabelled as English is still treated as Chinese.
- **Chinese titles** are written in Traditional characters (Taiwan forms) by
  default. `--chinese simplified` uses Simplified; `--chinese keep` uses each
  title exactly as stored. Only characters are converted, never words.
  Japanese titles are never converted.

### Online lookup (`--lookup`)

Looks each book up in [Bangumi](https://bgm.tv) (Chinese and Japanese books),
[Google Books](https://books.google.com) and
[Open Library](https://openlibrary.org), in that order. A catalog title is
only used when it is in the same language and nearly identical to the book's
own title; otherwise the book's title is kept. For Chinese books, a Taiwan
edition is preferred when one is found.

When a catalog title replaces a book's title, `--apply` also writes the new
title into the epub's metadata and keeps the old title as an alternate title.
Everything else in the file is left byte-for-byte unchanged.

- **Privacy:** `--lookup` sends each book's title, authors and ISBN to these
  services. Without `--lookup`, epub-tools makes no network requests.
- **Google Books** refuses most requests without an API key. To use it, create
  a key in the [Google Cloud console](https://console.cloud.google.com/apis/library/books.googleapis.com)
  and set `GOOGLE_BOOKS_API_KEY`.

## Development

```sh
go test ./...
go run ./cmd/epub-tools rename --help
```

Tests that read real books from `testdata/` are skipped when those files are
absent (they are not committed). See [RELEASING.md](RELEASING.md) for how
releases are made.

## License

MIT — see [LICENSE](LICENSE).
