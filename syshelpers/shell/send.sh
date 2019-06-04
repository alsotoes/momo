#!/bin/bash

DIR=$1
count=1

for file in $(ls -1 ${DIR})
do
    ./momo -file ${DIR}/${file} > /dev/null 2>&1 &
    count=`expr $count + 1`
    if [[ "$count"%1000 -eq 0 ]]; then
        sleep 1
    fi
done
