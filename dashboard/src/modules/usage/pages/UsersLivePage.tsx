import { Centered, EmptyState, Spinner, periodLabel } from '../../../common';
import { useUserUsage } from '../hooks/useUsage';
import { UNATTRIBUTED } from '../utils';
import { UsersPage, type User } from './UsersPage';

interface UsersLivePageProps {
  range: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
}

export function UsersLivePage({ range, setView, setSelected }: UsersLivePageProps) {
  const { data, isLoading, isError, dataUpdatedAt } = useUserUsage(range);

  if (isLoading) {
    return (
      <Centered>
        <Spinner />
      </Centered>
    );
  }
  if (isError) {
    return <Centered>Failed to load clients</Centered>;
  }

  const items = data?.items ?? [];
  if (items.length === 0) {
    return (
      <EmptyState
        message="No clients yet"
        description="Clients appear as your workflows report activity tagged with a user.id."
      />
    );
  }

  const users: User[] = items.map((t) => ({
    id: t.user_id || UNATTRIBUTED,
    name: t.user_id || 'default',
    cost: t.cost,
    runs: t.runs,
    avg: t.runs > 0 ? t.cost / t.runs : 0,
    trend: t.trend ?? [],
  }));

  const comparison = {
    runs: items.reduce((s, t) => s + t.runs_prev, 0),
    cost: items.reduce((s, t) => s + t.cost_prev, 0),
  };

  return (
    <UsersPage
      users={users}
      periodLabel={periodLabel(range)}
      comparison={comparison}
      updatedAt={dataUpdatedAt}
      setView={setView}
      setSelected={setSelected}
    />
  );
}

