import { useState, useEffect } from 'react';
import { DataQuality, fetchDataQuality } from '../utils/api';

export function useDataQuality(): DataQuality | null {
  const [quality, setQuality] = useState<DataQuality | null>(null);

  useEffect(() => {
    fetchDataQuality().then(setQuality).catch(() => {
      setQuality({ pricingSource: 'unknown', message: '' });
    });
  }, []);

  return quality;
}
