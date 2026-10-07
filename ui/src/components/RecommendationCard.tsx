import React from 'react';
import { AdviceCard, AdviceNote } from './AdviceCard';
import { InfoIcon } from './icons';
import { useTokens } from '../theme/tokens';
import { CpuRecommendation } from '../utils/costInsights';
import { formatCores, formatMillicores, formatMoney } from '../utils/format';

interface Props {
  recommendation: CpuRecommendation;
  // controllerName is set when the application has a single controller, so the advice can name it.
  controllerName?: string | null;
  // namespaceName is set when the advice covers a whole namespace rather than one application.
  namespaceName?: string;
  onShowResources?: () => void;
  // banner spans a page and puts the button beside the text, wrapping it underneath when there is
  // no room; column stacks everything for a card column. flush drops the space below the card.
  layout?: 'banner' | 'column';
  flush?: boolean;
}

export const RecommendationCard: React.FC<Props> = ({
  recommendation,
  controllerName = null,
  namespaceName,
  onShowResources,
  layout = 'banner',
  flush = false,
}) => {
  const t = useTokens();
  const { requestedCores, averageCores, peakCores, suggestedCores, monthlySavings } = recommendation;
  const requested = formatCores(requestedCores);
  const average = formatCores(averageCores);

  let title: string;
  let usage: string;
  if (namespaceName) {
    title = `Lower CPU requests in ${namespaceName}`;
    usage = `Pods request ${requested} and use ${average} on average.`;
  } else if (controllerName) {
    title = `Lower the CPU request of ${controllerName}`;
    usage = `It requests ${requested} and uses ${average} on average.`;
  } else {
    title = 'Lower the CPU requests of this application';
    usage = `Its pods request ${requested} and use ${average} on average.`;
  }
  const basis =
    peakCores === null
      ? "Peak CPU isn't collected, so check peaks before lowering the request."
      : `This leaves 25% headroom over the peak of ${formatCores(peakCores)}.`;

  return (
    <AdviceCard
      title={title}
      notes={<AdviceNote icon={<InfoIcon color={t.textMuted} />}>{basis}</AdviceNote>}
      onShowResources={onShowResources}
      layout={layout}
      flush={flush}
    >
      {usage} Requesting {formatMillicores(suggestedCores)} would save about {formatMoney(monthlySavings, 'USD', 0)}/mo.
    </AdviceCard>
  );
};
