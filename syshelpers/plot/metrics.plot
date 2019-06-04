echo "
  set term x11
  set datafile separator ','
  set title 'Metrics'
  set grid
  set style circle radius screen 0.0001
  plot 'disk.csv' using $1
" | gnuplot --persist

