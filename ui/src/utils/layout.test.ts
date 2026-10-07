import { describe, expect, it } from 'vitest';
import { chartHeight, dayLabelCount, MIN_STAT_WIDTH, statColumns } from './layout';

describe('statColumns', () => {
  it('keeps every stat on one row while each has room', () => {
    expect(statColumns(4 * MIN_STAT_WIDTH, 4)).toBe(4);
    expect(statColumns(2330, 4)).toBe(4);
  });

  it('folds to two columns, then one', () => {
    expect(statColumns(4 * MIN_STAT_WIDTH - 1, 4)).toBe(2);
    expect(statColumns(2 * MIN_STAT_WIDTH, 3)).toBe(2);
    expect(statColumns(2 * MIN_STAT_WIDTH - 1, 3)).toBe(1);
  });

  it('never splits a single stat', () => {
    expect(statColumns(0, 1)).toBe(1);
  });

  it('treats an unmeasured strip as wide', () => {
    expect(statColumns(Number.POSITIVE_INFINITY, 4)).toBe(4);
  });
});

describe('chartHeight', () => {
  it('stays at 240px up to the widths charts had before', () => {
    expect(chartHeight(600)).toBe(240);
    expect(chartHeight(1200)).toBe(240);
  });

  it('grows with the width to keep a 5:1 shape', () => {
    expect(chartHeight(1600)).toBe(320);
    expect(chartHeight(2250)).toBe(450);
  });

  it('stops at 450px', () => {
    expect(chartHeight(4000)).toBe(450);
  });
});

describe('dayLabelCount', () => {
  it('matches the seven labels charts had at their usual width', () => {
    expect(dayLabelCount(1132)).toBe(7);
  });

  it('adds labels as the plot widens, up to 15', () => {
    expect(dayLabelCount(2182)).toBe(13);
    expect(dayLabelCount(5000)).toBe(15);
  });

  it('keeps at least three on a narrow plot', () => {
    expect(dayLabelCount(200)).toBe(3);
  });
});
