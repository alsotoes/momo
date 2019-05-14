#!/bin/bash
#   To create 100000 small files between 1b and 1024b and stored in ultra_small_3 directory
#   sh create_ultra_small_files.sh 1000 ultra_small_3


MAX=$1
DIR=$2

for i in $(seq $MAX)
do
    for y in $(shuf -i 1-1024 -n 100)
    do
        name=$(uuidgen)
        dd if=/dev/urandom of=./$2/$name bs=${y} count=1
    done
done

