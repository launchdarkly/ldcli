import { useState, useEffect, useCallback } from 'react';
import {
  Label,
  ListBox,
  ListBoxItem,
  Input,
  ProgressBar,
} from '@launchpad-ui/components';
import { Box, Stack } from '@launchpad-ui/core';
import { fetchEnvironments } from './api';
import { Environment } from './types';
import debounce from 'lodash/debounce';

type Props = {
  projectKey: string;
  sourceEnvironmentKey: string | null;
  selectedEnvironment: Environment | null;
  setSelectedEnvironment: (environment: Environment | null) => void;
};

export function EnvironmentSelector({
  projectKey,
  sourceEnvironmentKey,
  selectedEnvironment,
  setSelectedEnvironment,
}: Props) {
  const [environments, setEnvironments] = useState<Environment[] | null>(null);

  const [searchQuery, setSearchQuery] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  const fetchEnvironmentsDebounced = useCallback(
    debounce((query: string) => {
      setIsLoading(true);
      fetchEnvironments(projectKey, query)
        .then((envs) => {
          setEnvironments(envs);
          if (!selectedEnvironment) {
            const sourceEnv = envs.find(
              (env) => env.key === sourceEnvironmentKey,
            );
            if (sourceEnv) {
              setSelectedEnvironment(sourceEnv);
            } else if (envs.length > 0) {
              setSelectedEnvironment(envs[0]);
            }
          }
        })
        .catch((error) => {
          console.error('Error fetching environments:', error);
        })
        .finally(() => {
          setIsLoading(false);
        });
    }, 300),
    [
      projectKey,
      sourceEnvironmentKey,
      selectedEnvironment,
      setSelectedEnvironment,
    ],
  );

  useEffect(() => {
    fetchEnvironmentsDebounced(searchQuery);
  }, [fetchEnvironmentsDebounced, searchQuery]);

  return (
    <Stack gap="3">
      <Box display="flex" justifyContent="space-between" alignItems="center">
        <Label
          htmlFor="environmentSearch"
          style={{ fontSize: '1rem', fontWeight: 'bold' }}
        >
          Environments
        </Label>
      </Box>
      <Box display="flex" justifyContent="space-between" alignItems="center">
        <div style={{ position: 'relative', flexGrow: 1 }}>
          <Input
            id="environmentSearch"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value || '')}
            placeholder="Search environments..."
            aria-label="Search environments"
          />
          {isLoading && (
            <Box
              style={{
                position: 'absolute',
                right: '0.5rem',
                top: '50%',
                transform: 'translateY(-50%)',
              }}
            >
              <ProgressBar size="small" aria-label="Loading environments" />
            </Box>
          )}
        </div>
        <span
          style={{
            whiteSpace: 'nowrap',
            flexShrink: 0,
            marginLeft: '0.5rem',
          }}
        >
          {selectedEnvironment?.name || 'No environment selected'}
        </span>
      </Box>

      <Box
        style={{
          height: '12.5rem',
          overflowY: 'auto',
          backgroundColor: 'var(--lp-color-bg-ui-secondary)',
          borderRadius: '0.5rem',
          border: '0.0625rem solid var(--lp-color-border-ui-primary)',
        }}
      >
        <ListBox
          aria-label="Environments"
          selectionMode="single"
          selectedKeys={[selectedEnvironment?.key || '']}
          onSelectionChange={(keys) => {
            const selectedKey = Array.from(keys)[0] as string;
            const selected = environments?.find(
              (env) => env.key === selectedKey,
            );
            if (selected) {
              setSelectedEnvironment(selected);
            }
          }}
        >
          {environments?.map((env) => (
            <ListBoxItem key={env.key} id={env.key}>
              {env.name}
            </ListBoxItem>
          ))}
        </ListBox>
      </Box>
    </Stack>
  );
}
