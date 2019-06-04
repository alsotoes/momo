#!/bin/bash

for i in $(find ../ -name "*.go");do cat $i | wc -l; done | paste -sd+ - | bc
