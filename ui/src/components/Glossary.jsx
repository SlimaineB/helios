import Modal from './Modal.jsx'

const TERMS = [
  {
    term: 'Application',
    definition: 'Ansible content tracked at a revision (branch, tag or commit). Creating an application does not run anything until it is assigned to an instance.',
  },
  {
    term: 'Instance / host',
    definition: 'A managed machine. “Instance” is the Helios record for a host, including its project, execution mode and assigned applications.',
  },
  {
    term: 'Assignment',
    definition: 'The link between one application and one instance. Variables can be overridden per assignment.',
  },
  {
    term: 'Check now',
    definition: 'Runs the playbook in check mode. It reports drift and produces a diff without changing the host.',
  },
  {
    term: 'Apply now',
    definition: 'Runs the playbook for real and converges the host to the desired state.',
  },
  {
    term: 'Check interval',
    definition: 'How often the agent checks this application for drift. 0 inherits the agent poll interval; Apply stays manual unless auto-apply is enabled.',
  },
  {
    term: 'Drift',
    definition: 'A check found differences between the desired configuration and the current host state.',
  },
  {
    term: 'Review drift',
    definition: 'Opens the latest check diff so you can inspect what would change before applying it.',
  },
  {
    term: 'Auto-apply on drift',
    definition: 'When a check detects drift, Helios queues an Apply automatically. Leave it off when changes need operator review.',
  },
  {
    term: 'Stuck',
    definition: 'Auto-apply is paused after repeated failures. Fix the playbook, then resume auto-apply.',
  },
  {
    term: 'Run',
    definition: 'One Check or Apply execution, with its status, log, revision and optional diff.',
  },
  {
    term: 'Sync window',
    definition: 'A schedule that allows or blocks Apply. Checks and drift detection always run.',
  },
  {
    term: 'Project',
    definition: 'A scope for RBAC, repositories, applications and hosts. Global resources use the default project.',
  },
  {
    term: 'GitOps config repo',
    definition: 'A Git repository that declares projects, applications and assignments. Reconciling it updates Helios.',
  },
  {
    term: 'Tunnel / bastion',
    definition: 'A host reached through a reverse SSH tunnel and a runner on the bastion. A lightweight tunnel agent can keep the tunnel open; Ansible runs on the bastion.',
  },
  {
    term: 'Rollback',
    definition: 'Pins an assignment to a previous commit and applies it again. Track branch resumes normal revision tracking.',
  },
]

export default function Glossary({ onClose }) {
  return (
    <Modal title="Glossary & how it works" onClose={onClose} wide>
      <p className="glossary-flow">
        Start with <strong>Check now</strong> to detect drift without changing the host. Use{' '}
        <strong>Review drift</strong> to inspect the diff, then <strong>Apply now</strong> when the change is safe.
      </p>
      <dl className="glossary">
        {TERMS.map(({ term, definition }) => (
          <div className="glossary-item" key={term}>
            <dt>{term}</dt>
            <dd>{definition}</dd>
          </div>
        ))}
      </dl>
    </Modal>
  )
}
