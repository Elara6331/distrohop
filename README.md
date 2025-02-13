<p align="center">
    <img width="250" src="assets/logo/distrohop-text-bottom.svg">
</p>

---

**DistroHop** is a tool that helps you compare Linux packages across different distributions. It lets you search for a package from one distro in another's repositories or look for a specific item you need.

## How does it work?

Distrohop works by downloading and decoding a file index from each supported repo. It analyzes the information contained in the index to form a generalized list of tags describing the contents of each package, and then stores that list in a database.

When you search for a package from another distro, it resolves the package name to its list of tags, and then searches for any packages that match at least one tag in the other distro's repos. It calculates a confidence score based on how many of the tags match, and then sorts the results by confidence.

## Why are some searches so slow?

Each repo can have tens of millions of tags that Distrohop has to churn through. It uses LSM trees and bloom filters to speed the search up as much as possible, and most searches can be measured in milliseconds, but for some searches that contain lots of tags, there may not be any shortcut and DistroHop may have to scan through all or most of the tags stored in the database, which can take a long time.

## Installation

You can either install one of the distro packages in the [latest Gitea release](https://gitea.elara.ws/Elara6331/distrohop/releases/latest) or the [Docker container](https://gitea.elara.ws/elara6331/-/packages/container/distrohop/latest). This repo contains an [example compose file](docker-compose.yml).

The distro packages look for your config file in `$XDG_CONFIG_HOME` and store data in `$XDG_DATA_HOME`, or your OS's equivalent.

The container looks for your config file at `/distrohop.toml` and stores data in `/data`, so make sure to create volumes for those. You can choose to use environment variables instead of a config file if you'd like. You can see an example docker-compose file [here](docker-compose.yml).

## Configuration

DistroHop's config file consists of a list of distro repositories. Here's an excerpt from the [example config](distrohop.toml) provided in this repo:

```toml
[[repo]]
    refresh_schedule = "0 0 * * *" # Every day at 12:00 AM
    name = "debian-bookworm"
    type = "apt"
    base_url = "http://ftp.us.debian.org/debian"
    version = "bookworm"
    repos = ["main", "non-free", "contrib"]
    arch = ["amd64", "all"]
```

- `refresh_schedule` is a crontab string that represents the schedule by which the repo will be updated. All repos will also always be updated on startup. The default for this setting is `0 0 * * *`, which means every day at 12:00 AM.
- `name` is the name that you'd like DistroHop to reference the repo by.
- `type` is one of `apt`, `dnf`, or `pacman`.
- `base_url` is the base URL of the repo. For Arch, it accepts variables such as `$repo` and `$arch` which will be replaced with the repo/arch value currently being pulled.
- `version` is the distro-specific repo version string. For Debian, this is the release codename (`buster`, `bullseye`, `bookworm`, `trixie`, etc.). Arch doesn't use this variable, so it can be omitted in Arch repos.
- `repos` is a list of distro-specific repo names. All Ubuntu versions and Debian versions before Wheezy don't use this, and it should be omitted in those repos to avoid duplicate downloads.
- `arch` is a list of distro-specific binary architectures for which indices should be pulled.

There's also a top-level setting outside of any repos called `search_threads`, which is an integer specifying how many threads should be spawned for database searches. The default is `4`.

All the config settings can also be set through environment variables, like this:

```bash
DISTROHOP_SEARCH_THREADS=4
DISTROHOP_REPO_0_REFRESH_SCHEDULE="0 0 * * *"
DISTROHOP_REPO_0_NAME="debian-bookworm"
DISTROHOP_REPO_0_TYPE="apt"
DISTROHOP_REPO_0_BASE_URL="http://ftp.us.debian.org/debian"
DISTROHOP_REPO_0_VERSION="bookworm"
DISTROHOP_REPO_0_REPOS="main,non-free,contrib"
DISTROHOP_REPO_0_ARCH="amd64,all"
```

## Attribution

All the icons stored under `assets/icons` are downloaded from various icon packs on https://iconify.design.