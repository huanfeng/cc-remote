// The page's send IDs, shared by the composer and the quick switch: held
// across sessions and remounts, a message keeps its ID until the host takes it.
import { SendIds } from './composerLogic';

export const sendIds = new SendIds();
