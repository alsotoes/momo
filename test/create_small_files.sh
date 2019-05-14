#!/bin/bash
#   To create 100000 small files between 1K and 100K and stored in small_3 directory
#   sh create_small_files.sh 1000 small_3

MAX=$1
DIR=$2

for i in $(seq $MAX)
do
    for y in $(shuf -i 1-100 -n 100)
    do
        name=$(uuidgen)
        dd if=/dev/urandom of=./$2/$name bs=${y}k count=1
    done
done

