# OpenLinkHub-deck

An **unofficial**, native Linux x86-64 OpenDeck plugin that displays Corsair device battery percentages reported by the [OpenLinkHub](https://github.com/jurkovic-nikola/OpenLinkHub) API.

Not affiliated with or endorsed by OpenLinkHub, OpenDeck, or Corsair.

## Features

- Battery percentage, battery bar, configurable device name/type and positions, font sizes
- Configurable OpenLinkHub IP and port (default `127.0.0.1:27003`)
- Offline state displaying `Device Offline`

**Charging detection is only an estimate.** An increase in the reported battery percentage does **not** prove the device is charging now. Battery reports may be delayed, coarse, or inaccurate. The indication resets when the plugin restarts.

## Installation

Download the `OpenLinkHub-deck-1.1.2.zip` release asset, then use OpenDeck's **Install from file** option. OpenLinkHub must be running and accessible from OpenDeck.

## License

**A public redistribution license has not yet been selected.** Review code and asset provenance and choose a license before publishing this repository as an open-source project or submitting to the OpenAction plugin catalogue.
