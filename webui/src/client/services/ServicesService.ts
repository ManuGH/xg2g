// Legacy ServicesService compatibility wrapper
import * as api from '../../client-ts';

export const ServicesService = {
  getServicesBouquets: api.getServicesBouquets,
  getServices: api.getServices,
  postServicesByIdToggle: api.postServicesByIdToggle,
};
