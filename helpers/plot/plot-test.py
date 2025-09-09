#!/usr/bin/env python3
import matplotlib.pyplot as plt
import pandas as pd
from dateutil.parser import parse
from datetime import datetime, date

def to_timestamp(x):
    dt = parse(x)
    
    year = str(dt.date()).split("-")[0]
    month = str(dt.date()).split("-")[1]
    day = str(dt.date()).split("-")[2]

    hour = str(dt.time()).split(":")[0]
    minute = str(dt.time()).split(":")[1]
    seconds = str(dt.time()).split(":")[2]

    timestamp = datetime(int(year), int(month), int(day), int(hour), int(minute), int(seconds)).timestamp()
    return int(timestamp)


data = pd.read_csv(open("/Users/kevinflynn/Downloads/node_cpu_seconds_total_SERVER-data-2020-07-05_20_47_14.csv"), quotechar='"', skipinitialspace=True)
data['Time'] = data['Time'].apply(to_timestamp)
with pd.option_context('display.precision', 15):
    plt.figure()
    #print(data['Time'])
    #print(data)

    # boxplot 
    plt.boxplot(data["cpu-momo-server0"]) 
     
    # show plot 
    plt.show() 
