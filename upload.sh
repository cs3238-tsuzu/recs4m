#! /bin/bash

set -eu

eyeD3 -t "$2($3)" -A $2 $1
gmupload --uploader-id=2E:86:E6:63:09:2B $1