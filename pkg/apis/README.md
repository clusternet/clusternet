# apis

Schema of the external API types that are served by Clusternet.

## Purpose

This library is the canonical location of the Clusternet API definition. Most likely interaction with this repository is
used as a dependency.

## Where does it come from?

`apis` is synced from <https://github.com/clusternet/clusternet/tree/main/pkg/apis>. Code changes are made in that
location, merged into `clusternet/clusternet` and later synced here.

## Things you should *NOT* do

1. Directly modify any files under this repo. Those are driven
   from <https://github.com/clusternet/clusternet/tree/main/pkg/apis>.
2. Expect compatibility. This repo is changing quickly in direct support of Clusternet and the API isn't yet stable
   enough for API guarantees.

## Recommended Use

We recommend using the go types in this repo. You may serialize them directly to JSON.
