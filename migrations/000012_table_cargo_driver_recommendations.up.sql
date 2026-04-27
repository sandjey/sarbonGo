--
-- PostgreSQL database dump
--

\restrict mjAwV81aNpbFxlh83JAn7iEWmTbPAKo8WIODcDnhCtRt4nTwsqNi4dRTOdjhL9v

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
-- Name: cargo_driver_recommendations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cargo_driver_recommendations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    cargo_id uuid NOT NULL,
    driver_id uuid NOT NULL,
    invited_by_dispatcher_id uuid NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_cargo_id_driver_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_cargo_id_driver_id_key UNIQUE (cargo_id, driver_id);


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_pkey PRIMARY KEY (id);


--
-- Name: idx_cargo_driver_recommendations_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_driver_recommendations_cargo ON public.cargo_driver_recommendations USING btree (cargo_id);


--
-- Name: idx_cargo_driver_recommendations_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_driver_recommendations_dispatcher ON public.cargo_driver_recommendations USING btree (invited_by_dispatcher_id);


--
-- Name: idx_cargo_driver_recommendations_driver; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cargo_driver_recommendations_driver ON public.cargo_driver_recommendations USING btree (driver_id);


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_driver_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES public.drivers(id) ON DELETE CASCADE;


--
-- Name: cargo_driver_recommendations cargo_driver_recommendations_invited_by_dispatcher_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cargo_driver_recommendations
    ADD CONSTRAINT cargo_driver_recommendations_invited_by_dispatcher_id_fkey FOREIGN KEY (invited_by_dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict mjAwV81aNpbFxlh83JAn7iEWmTbPAKo8WIODcDnhCtRt4nTwsqNi4dRTOdjhL9v

