# OpenLinkHub-deck

An **unofficial**, native Linux x86-64 OpenDeck plugin that displays Corsair device battery percentages reported by the [OpenLinkHub](https://github.com/jurkovic-nikola/OpenLinkHub) API.

Not affiliated with or endorsed by OpenLinkHub, OpenDeck, or Corsair.

## Features

- Original Standard style, with a dark blue-tinted background
- English device types and English property inspector labels
- Battery percentage, battery bar, configurable device name/type and positions, font sizes
- Configurable OpenLinkHub IP and port (default `127.0.0.1:27003`)
- Offline state displaying `Device Offline`
- Black right-pointing chevrons inside the bar after a reported battery increase, for up to ten minutes; never at 100%

**Charging detection is only an estimate.** An increase in the reported battery percentage does **not** prove the device is charging now. Battery reports may be delayed, coarse, or inaccurate. The indication resets when the plugin restarts.

## Installation

Download the `OpenLinkHub-deck-1.1.2.zip` release asset, then use OpenDeck's **Install from file** option. OpenLinkHub must be running and accessible from OpenDeck.

## Build from source

Requires Go for building only (not on the user's computer):

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o com.local.corsairbattery.sdPlugin/openlinkhub-deck main.go
```

Package the `com.local.corsairbattery.sdPlugin` directory into a ZIP, preserving the directory name and the executable bit on `openlinkhub-deck`.

## License

**A public redistribution license has not yet been selected.** Review code and asset provenance and choose a license before publishing this repository as an open-source project or submitting to the OpenAction plugin catalogue.
