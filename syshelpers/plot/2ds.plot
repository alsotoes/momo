echo "
  set datafile separator ','
  set ylabel 'Data set size'
  set xlabel 'Sort time (sec)'
  set title '$1 v/s $2'
  set grid
  set style circle radius screen 0.01
  plot '$1' using 1:2 with lines lc rgb 'forest-green', \
    '$2' using 1:2 with lines lc rgb 'blue'
" | gnuplot --persist



