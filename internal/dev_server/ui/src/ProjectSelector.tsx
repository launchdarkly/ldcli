import { useEffect, useState } from 'react';
import { apiRoute } from './util.ts';
import {
  Button,
  Heading,
  Menu,
  MenuItem,
  MenuTrigger,
  Popover,
  ProgressBar,
  Text,
  Tooltip,
  TooltipTrigger,
} from '@launchpad-ui/components';
import { Alert, CopyToClipboard, Inline } from '@launchpad-ui/core';

const fetchProjects = async () => {
  const res = await fetch(apiRoute(`/dev/projects`));
  const json = await res.json();
  if (!res.ok) {
    throw new Error(`Got ${res.status}, ${res.statusText} from projects fetch`);
  }
  return json;
};

type Props = {
  selectedProject: string | null;
  setSelectedProject: (selectedProject: string) => void;
  setShowBanner: (showBanner: boolean) => void;
};

function ProjectSelector({
  selectedProject,
  setSelectedProject,
  setShowBanner,
}: Props) {
  const [projects, setProjects] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const setProjectsAndUpdateSelectedProject = (projects: string[]) => {
    setProjects(projects);
    setShowBanner(projects.length == 0);
    if (projects.length == 1) {
      setSelectedProject(projects[0]);
    }
    setIsLoading(false);
  };

  useEffect(() => {
    fetchProjects()
      .then(setProjectsAndUpdateSelectedProject)
      .catch((error) => {
        console.error(error);
        setIsLoading(false); //bad
      });
    setProjects([]);
  }, []);

  if (isLoading) {
    return (
      <div style={{ textAlign: 'center' }}>
        <ProgressBar
          aria-label="loading"
          isIndeterminate
          style={{ width: '100%', height: '2rem' }}
        />
        <p>Projects are loading</p>
      </div>
    );
  }

  return projects.length > 0 ? (
    <MenuTrigger>
      <Inline gap="1">
        <TooltipTrigger>
          <Button>
            {selectedProject == null ? 'Select a project' : selectedProject}
          </Button>
          <Tooltip>
            {selectedProject == null
              ? 'Please select a project'
              : 'This is the selected project'}
          </Tooltip>
        </TooltipTrigger>
      </Inline>

      <Popover>
        <Menu>
          {projects.map((project) => (
            <MenuItem
              key={project}
              onAction={() => {
                setSelectedProject(project);
              }}
            >
              {project}
            </MenuItem>
          ))}
        </Menu>
      </Popover>
    </MenuTrigger>
  ) : (
    <Alert kind="error">
      <Heading>No projects.</Heading>
      <Text>Add one via</Text>
      <CopyToClipboard kind="basic" text="ldcli dev-server add-project --help">
        ldcli dev-server add-project --help
      </CopyToClipboard>
    </Alert>
  );
}

export default ProjectSelector;
