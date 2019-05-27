#!/bin/bash

DIR=$1

for file in $(ls -1 ${DIR})
do
    ./momo -file ${DIR}/${file}
done
