#!/bin/bash

DIR=$1
#MAX=100000
MAX=25000
count=1

sleep $(shuf -i 1-200 -n 1)
for file in $(ls -1 ${DIR})
do
    ./momo -file ${DIR}/${file} > /dev/null 2>&1 &
    if [[ "$count"%1000 -eq 0 ]]; then
        sleep 1
    fi

    if [[ "$count" -eq "${MAX}" ]]; then
        exit
    fi
    count=`expr $count + 1`
done
