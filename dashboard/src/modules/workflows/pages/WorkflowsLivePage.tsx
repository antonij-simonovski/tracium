import { Centered, EmptyState, Spinner } from '../../../common';
import { useWorkflows } from '../hooks/useWorkflows';
import { WorkflowsPage } from './WorkflowsPage';

interface WorkflowsLivePageProps {
  range: string;
  setView: (v: string) => void;
  setSelected: (updater: (prev: Record<string, string>) => Record<string, string>) => void;
  workspaceName?: string;
}

export function WorkflowsLivePage({ range, setView, setSelected, workspaceName }: WorkflowsLivePageProps) {
  const { data, isLoading, isError, dataUpdatedAt } = useWorkflows(range);

  if (isLoading) {
    return (
      <Centered>
        <Spinner />
      </Centered>
    );
  }
  if (isError && !data) {
    return <Centered>Failed to load workflows</Centered>;
  }

  const workflows = data?.items ?? [];
  if (workflows.length === 0) {
    return (
      <EmptyState
        message="No workflows yet"
        description="Workflows appear here once they start emitting traces."
      />
    );
  }

  return (
    <WorkflowsPage
      workflows={workflows}
      range={range}
      setView={setView}
      setSelected={setSelected}
      workspaceName={workspaceName}
      updatedAt={dataUpdatedAt}
    />
  );
}
