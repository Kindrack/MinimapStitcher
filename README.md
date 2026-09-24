# MinimapStitcher

MinimapStitcher stitches World of Warcraft minimap tiles into complete map images.

It is designed to work with minimap files extracted from World of Warcraft data, including PNG and supported BLP textures. It can also use `md5translate.trs` files to resolve hash-named extracted files back to their original WoW paths.

The program is intended to be usable without manually renaming files or converting every BLP texture to PNG first.

## What it supports

### Input formats

- `.png` minimap tiles
- `.blp` WoW BLP2 minimap textures
- Hash-named `.png` or `.blp` files when a matching `md5translate.trs` is available
- `md5translate.trs` translation files

The built-in BLP reader supports the BLP2 formats used by the stitcher, including paletted, DXT-compressed, and BGRA data. JPEG-based BLP files are not supported by the built-in decoder.

### Folder names

The program automatically looks for either:

```text
Minimaps
Minimap
```

The search is case-insensitive. If both folders exist, `Minimaps` is preferred.

You do not need to rename the folder.

An explicit source directory can also be supplied as the first command-line argument.

## Basic usage

Put the executable beside your extracted minimap folder:

```text
MyWoWFiles/
├── MinimapStitcher.exe
├── Minimaps/
│   ├── Azeroth_24_53.png
│   ├── Azeroth_24_54.png
│   └── ...
└── StitchedMaps/
```

Run:

```text
MinimapStitcher.exe
```

The program automatically finds `Minimaps` and writes the resulting images to:

```text
StitchedMaps/
```


### Command-line arguments

The optional command-line form is:

```text
MinimapStitcher.exe [source] [destination]
```

For example:

```text
MinimapStitcher.exe "D:\WoW\Extracted\Minimap" "D:\WoW\Maps"
```

When no source is supplied, the program automatically searches the current directory for `Minimaps` or `Minimap`.

## File discovery

The stitcher recursively scans the selected source folder.

It ignores directories named `WMO`, because WMO minimap data is outside the intended minimap stitching workflow.

Files that cannot be understood as minimap tiles are ignored rather than causing the whole run to fail.

This allows a real extracted WoW directory to contain unrelated files without preventing usable minimap files from being stitched.

## Coordinate-based minimaps

The stitcher supports filenames such as:

```text
Azeroth_24_53.png
Azeroth_24_54.png
Azeroth_25_53.png
```

and equivalent BLP files:

```text
Azeroth_24_53.blp
Azeroth_24_54.blp
```

The numeric portions are treated as the tile coordinates.

When `mapX_Y` files are stored inside a map-named directory, the directory name is used as the map name.

For flat `mapX_Y` files without a useful containing directory, the output is named `WorldMap.png`.

## Recursive directory layouts

Files do not have to be directly inside the root minimap folder.

For example, this is supported:

```text
Minimaps/
├── Azeroth/
│   ├── map24_53.blp
│   └── map24_54.blp
└── Kalimdor/
    ├── map27_12.blp
    └── map27_13.blp
```

The program searches recursively and groups tiles according to their map/file naming information.

## 12-tile atlas sets

The stitcher also supports 12-image minimap sets arranged as a 4 × 3 atlas.

### Numbered sets with an underscore

For example:

```text
AhnQiraj1_1.png
AhnQiraj1_2.png
...
AhnQiraj1_12.png
```

This produces:

```text
AhnQiraj1.png
```

Each numbered set is treated as its own atlas.

The stitcher accepts both 1-based and 0-based tile numbering. For example, these are both valid:

```text
GillijimsIsle_0.png ... GillijimsIsle_11.png
GillijimsIsle_1.png ... GillijimsIsle_12.png
```

The same numbering forms are also accepted without the separator:

```text
GillijimsIsle0.png ... GillijimsIsle11.png
GillijimsIsle1.png ... GillijimsIsle12.png
```

All four forms produce the same 4 × 3 atlas layout. For numbered-set names such as `AhnQiraj1_0` through `AhnQiraj1_11`, the set name is `AhnQiraj1` and the tile numbers are normalized in the same way.

### Plain numbered sets

For example:

```text
ArathiBasin1.png
ArathiBasin2.png
...
ArathiBasin12.png
```

This produces:

```text
ArathiBasin.png
```

## Tile sizes

Tile dimensions are detected from the input files rather than being hard-coded.

For coordinate maps, the first usable tile establishes the expected tile dimensions. Other tiles in that map are checked against that size.

For atlas sets, all 12 tiles must have the same square dimensions.

The program reports the detected tile size and resulting output dimensions, for example:

```text
[Azeroth] tile size: 256x256, grid: 4x3, output: 1024x768
```

## Transparent tiles

Tiles are composited using alpha-aware `draw.Over` compositing.

This prevents transparent pixels in later tiles from incorrectly replacing already-drawn image data.

## Missing tiles

Coordinate maps do not require every coordinate between the minimum and maximum values to be present.

Missing positions are left empty/transparent in the output image.

The program reports the number of missing tile positions after stitching.

For example:

```text
[Azeroth] stitched with 0 decode error(s), 3 missing tile position(s).
```

Atlas sets are different: a 12-tile atlas must contain all 12 numbered files before it is accepted as an atlas.

## Corrupt or unreadable tiles

A bad individual coordinate tile does not automatically discard the entire map.

If at least one tile can be decoded, the map can still be written with the failed tile position left empty. The program reports the decode error and missing position.

If every tile in a coordinate map fails to decode, that map is skipped.

This is useful when working with incomplete or partially damaged extracted WoW data.

## BLP support

MinimapStitcher can read supported `.blp` files directly, so a separate BLP-to-PNG conversion step is not required for supported BLP2 files.

For troubleshooting, inspection, or converting files for use in other programs, the following tool is useful:

- [BLPConverter](https://github.com/Kkthnx/BLPConverter) converts World of Warcraft BLP textures to and from PNG and supports batch conversion. Its documentation also describes BLP2 formats including RAW and DXT1/DXT3/DXT5.

If a particular BLP cannot be read by MinimapStitcher, converting it with BLPConverter can help determine whether the file uses a format outside the built-in decoder's supported set.

## `md5translate.trs` support

Some WoW extraction methods produce files whose names are 32-character MD5-style hashes instead of their original names.

For example:

```text
Azeroth\map31_29.blp    a23c3c734658d0e788db865eec0b0258.blp
```

The extracted file might instead be:

```text
a23c3c734658d0e788db865eec0b0258.png
```

The stitcher matches the hash independently of the `.blp` or `.png` extension, so the extension in the `.trs` file does not have to match the extracted file extension.

Only hashes for files actually present in the selected minimap directory are used. Entries in a large `.trs` file that refer to files not present in the current extraction are ignored.

If the same hash is mapped to conflicting logical paths, the hash is treated as ambiguous and is not used.

## Working with MPQ files

When working with older World of Warcraft installations or patches stored in MPQ archives, you first need to extract the relevant files.

[MPQ Editor](http://www.zezula.net/en/mpq/download.html) is a useful tool for opening and extracting files from MPQ archives.

A typical workflow is:

```text
WoW MPQ archive
      |
      v
 MPQ Editor
      |
      v
Extracted minimap files
      |
      v
MinimapStitcher
      |
      v
StitchedMaps/*.png
```

## WDB/database tools

[WDBX Editor](https://github.com/WowDevTools/WDBXEditor/) can be useful when researching World of Warcraft database files and related game data while working out how extracted map information corresponds to the game.

It is not required for MinimapStitcher itself.

## Recommended WoW file workflow

For a typical extraction workflow:

1. Use [MPQ Editor](http://www.zezula.net/en/mpq/download.html) to extract files from the WoW MPQ archives when applicable.
2. Keep the extracted minimap files in a `Minimap` or `Minimaps` directory.
3. Keep `md5translate.trs` with the extracted files when your extraction method generated hash-named files.
4. Run `MinimapStitcher.exe`.
5. If you need to inspect or manually convert BLP files, use [BLPConverter](https://github.com/Kkthnx/BLPConverter).
6. Use [WDBX Editor](https://github.com/WowDevTools/WDBXEditor) when investigating related WoW database/game-data information.

## Building from source

MinimapStitcher is written in Go and uses the Go standard library.

Build for the current operating system with:

```text
go build MinimapStitcher.go
```

## Final Notes

- The program does not modify the source minimap files.
- Files that cannot be understood are ignored.
- `WMO` directories are skipped.
- Supported BLP files can be read directly without first converting them to PNG.
- Hash translation works independently of whether the extracted file is stored as `.png` or `.blp`.
- A complete 12-tile atlas is required for atlas-style input. Atlas tiles may be numbered 0-11 or 1-12, with or without the underscore separator.
- Coordinate maps can contain gaps; missing positions are left empty.
