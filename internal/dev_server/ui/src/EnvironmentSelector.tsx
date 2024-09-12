import { useState, useEffect } from 'react';
import { Label, ListBox, ListBoxItem, Input } from '@launchpad-ui/components';
import { Box, Inline, Stack } from '@launchpad-ui/core';
import { fetchEnvironments } from './api';
import { Environment } from './types';

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
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [filteredEnvironments, setFilteredEnvironments] = useState<
    Environment[]
  >([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    setIsLoading(true);
    fetchEnvironments(projectKey)
      .then((envs) => {
        setEnvironments(envs);
        setFilteredEnvironments(envs);
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
  }, [projectKey, sourceEnvironmentKey]); // Remove selectedEnvironment and setSelectedEnvironment from dependencies

  useEffect(() => {
    const filtered = environments.filter(
      (env) =>
        env.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        env.key.toLowerCase().includes(searchQuery.toLowerCase()),
    );
    setFilteredEnvironments(filtered);
  }, [searchQuery, environments]);

  if (isLoading) {
    return <span>Loading environments...</span>;
  }

  console.log(searchQuery);

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
        <div>
          <Input
            id="environmentSearch"
            value={searchQuery}
            onChange={(e) => {
              console.log(e.target.value);
              setSearchQuery(e.target.value || '');
            }}
            placeholder="Search environments..."
            aria-label="Search environments"
          />
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
            const selected = environments.find(
              (env) => env.key === selectedKey,
            );
            if (selected) {
              setSelectedEnvironment(selected);
            }
          }}
        >
          {filteredEnvironments.map((env) => (
            <ListBoxItem key={env.key} id={env.key}>
              {env.name}
            </ListBoxItem>
          ))}
        </ListBox>
      </Box>
    </Stack>
  );
}
