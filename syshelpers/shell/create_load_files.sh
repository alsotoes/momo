#!/bin/bash
# for i in 4 8 16 32 64 128 512 1024 2048 4096; do sh ../../syshelpers/shell/create_load_files.sh 100 ./client0/${i}k/ ${i}; done

MAX=$1
DIR=$2
SIZ=$3

for i in $(seq $MAX)
do
    name=$(uuidgen)
    dd if=/dev/urandom of=./${DIR}/$name bs=${SIZ}k count=1
done

