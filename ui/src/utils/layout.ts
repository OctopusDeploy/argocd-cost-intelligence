// Layout rules shared by every view, so they all adapt to the space they get in the same way.
// Widths are the width of the element being laid out, never the window's: the same strip or table
// can fill a whole page or sit in a narrow column beside a list.

// A stat needs this much room for a figure such as "$12,345.67" plus its padding.
export const MIN_STAT_WIDTH = 220;

// statColumns folds a strip of `count` stats to fit `width`: one row, then two columns, then one.
export function statColumns(width: number, count: number): number {
  if (count <= 1) return 1;
  if (width >= count * MIN_STAT_WIDTH) return count;
  if (width >= 2 * MIN_STAT_WIDTH) return 2;
  return 1;
}

// Three card columns of about 600px, with their gaps, fit from this width (Actual costs).
export const THREE_CARD_COLUMNS_MIN = 1880;

// A namespace's figures and its recommendation share one row from this width.
export const PAIRED_ROW_MIN = 1300;

// Below this width the namespace list stacks above the selected namespace instead of beside it.
export const SIDE_LIST_MIN = 860;

// Table columns. The name column stops growing at 480px, and a trailing spacer track takes whatever
// width is left, so figures stay next to the names they belong to. The spacer only grows once the
// name column is full, so below that width tables lay out exactly as they did without it.
export const NAME_COLUMN = 'minmax(160px, 480px)';
export const SPACER_COLUMN = 'minmax(0, 1fr)';

// chartHeight keeps a chart at roughly 5:1: 20% of its width, between 240px and 450px.
export function chartHeight(width: number): number {
  return Math.round(Math.min(450, Math.max(240, width * 0.2)));
}

// dayLabelCount allows about one day label per 160px of plot, between 3 and 15 labels.
export function dayLabelCount(plotWidth: number): number {
  return Math.min(15, Math.max(3, Math.floor(plotWidth / 160)));
}
