--
-- PostgreSQL database dump
--

\restrict OyjvHovuooEXyhxOhSQrVoKt65CxGHWeT43QA3xoDVaeBc019i11mdDTh2YF4c7

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: driver_manager_relations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.driver_manager_relations (
    driver_id uuid NOT NULL,
    manager_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now()
);


--
-- Name: driver_manager_relations driver_manager_relations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_manager_relations
    ADD CONSTRAINT driver_manager_relations_pkey PRIMARY KEY (driver_id, manager_id);


--
-- Name: idx_driver_manager_relations_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_manager_relations_driver ON public.driver_manager_relations USING btree (driver_id);


--
-- Name: idx_driver_manager_relations_manager; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_driver_manager_relations_manager ON public.driver_manager_relations USING btree (manager_id);


--
-- Name: driver_manager_relations driver_manager_relations_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_manager_relations
    ADD CONSTRAINT driver_manager_relations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: driver_manager_relations driver_manager_relations_manager_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.driver_manager_relations
    ADD CONSTRAINT driver_manager_relations_manager_id_fkey FOREIGN KEY (manager_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict OyjvHovuooEXyhxOhSQrVoKt65CxGHWeT43QA3xoDVaeBc019i11mdDTh2YF4c7

