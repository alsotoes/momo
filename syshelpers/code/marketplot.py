#!/usr/bin/env python3
import matplotlib.pyplot as plt
import pandas as pd
import numpy as np
from dateutil.parser import parse
from datetime import datetime, date

price = pd.Series(np.random.randn(150).cumsum(),index=pd.date_range('2000-1-1', periods=150, freq='B'))
#print(price)

ma = price.rolling(20).mean()
mstd = price.rolling(20).std()

plt.figure()
plt.plot(price.index, price, 'k')
print(price.index)
plt.plot(ma.index, ma, 'b')

plt.fill_between(mstd.index, ma - 2 * mstd, ma + 2 * mstd,color='b', alpha=0.2)
plt.show()
