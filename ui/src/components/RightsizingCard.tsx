import React from 'react';
import { AdviceCard, AdviceNote } from './AdviceCard';
import { InfoIcon, WarningIcon } from './icons';
import { useTokens } from '../theme/tokens';
import { Rightsizing } from '../utils/api';
import { formatMoney } from '../utils/format';
import { formatMonthlyChange, listNames, summarize } from '../utils/rightsizing';

interface Props {
  rightsizing: Rightsizing;
  onShowResources?: () => void;
  layout?: 'banner' | 'column';
  flush?: boolean;
}

// RightsizingCard turns the VPA's recommendations into one piece of advice: which workloads to lower
// and what that saves, and which need more than they request and what that costs. The saving and the
// increase are stated apart, so a net figure never hides a workload that is short of resources.
export const RightsizingCard: React.FC<Props> = ({ rightsizing, onShowResources, layout = 'banner', flush = false }) => {
  const t = useTokens();
  const { lower, raise, saving, increase, lowConfidence } = summarize(rightsizing);
  const net = saving - increase;

  const title =
    net >= 1
      ? `Right-size ${listNames([...new Set([...lower, ...raise])])} to save about ${formatMoney(net, 'USD', 0)}/mo`
      : `Raise the requests of ${listNames(raise)}`;
  const one = raise.length === 1;

  return (
    <AdviceCard
      title={title}
      onShowResources={onShowResources}
      layout={layout}
      flush={flush}
      notes={
        <>
          <AdviceNote icon={<InfoIcon color={t.textMuted} />}>
            From the Vertical Pod Autoscaler, per container. Requests only; limits are left as they are.
          </AdviceNote>
          {lowConfidence && (
            <AdviceNote icon={<WarningIcon color={t.warning} />}>
              Low confidence: the VPA&apos;s range is still wide for some containers, so check again in a few days.
            </AdviceNote>
          )}
        </>
      }
    >
      {lower.length > 0 && (
        <>
          The VPA suggests lower requests for {listNames(lower)} ({formatMonthlyChange(-saving)}).{' '}
        </>
      )}
      {raise.length > 0 && (
        <>
          {listNames(raise)} {one ? 'uses' : 'use'} more than {one ? 'it requests' : 'they request'} (
          {formatMonthlyChange(increase)}), so {one ? 'its' : 'their'} pods can run short.
        </>
      )}
    </AdviceCard>
  );
};
